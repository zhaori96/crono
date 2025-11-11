# Notas de Arquitetura do `crono`

## Visão Geral
- `crono` foca em dois blocos composáveis: um buffer de capacidade fixa (`FixedBuffer`) e uma time wheel (`Wheel`). Um módulo posterior (`scheduler`) conectará ambos sem misturar responsabilidades extras em cada componente.
- As APIs públicas devem operar em tempo constante sempre que possível, pré-alocando estruturas internas para minimizar lixo na heap. A sincronização prefere `sync/atomic`; mutex só aparece em trechos críticos que não podem ser expressos atomicamente.

## Preparação do Estágio A1
- `FixedBuffer` ficará em `internal/buffer`, sinalizando API interna, enquanto camadas futuras podem se apoiar nela. Uma fachada exportada pode surgir depois, se necessário.
- Os slots e contadores do buffer usarão slices pré-dimensionados e inteiros atômicos; índices fazem wrap com aritmética modular para evitar realocações.
- Os modos de acesso (`Synchronous`, `Asynchronous`, `Strategic`) serão expostos via um `AccessMode` semelhante a enum para manter descobribilidade e evitar inteiros mágicos.

## Decisões Pendentes
- Definir a estratégia exata do modo síncrono (candidato: variável de condição guardada por mutex), mantendo os caminhos assíncronos lock-free com atomics.
- Escolher se os slots do `FixedBuffer` armazenam `any` ou uma interface concreta. A primeira versão usará `any` para maximizar reutilização; especializações ficam para quando as integrações exigirem.
- Determinar se mutex por slot será necessário na integração com a wheel; a meta inicial é evitar esse custo.

## Estágio A1 — Buffer Fixo
- `NewFixedBuffer` valida a capacidade mínima e materializa implementações distintas: `synchronousBuffer` (somente locks e condição) e `strategicBuffer` (atomics com fallback controlado). O modo `AccessModeAsynchronous` desativa completamente o fallback para manter desempenho determinístico.
- O `strategicBuffer` mantém contadores atômicos (`occupancy`) e índices (`head`, `tail`) que avançam com aritmética modular. Quando o `allowLock` está habilitado, as operações tentam 10 CAS antes de entrar no mutex e usar o mesmo crescimento exponencial de dormência que a versão legada.
- `Release` agora recebe o item devolvido; se a estrutura já estiver cheia algo inconsistente aconteceu, então propagamos `ErrReuseMismatch` para sinalizar resultado inesperado.
- `Close` sinaliza o fechamento antes de iniciar qualquer limpeza, impedindo novas operações; o gerenciamento de drenagem e limpeza foi detalhado no Estágio A2. No modo síncrono, o mutex permite limpar a memória com segurança imediata.

## Estágio A2 — Concurrency Refinements
- Introduzido `internal/util.ExponentialSleeper` para encapsular o backoff exponencial com timer reutilizável, aplicado em todas as rotinas que podem escalar para o mutex (`reserveSlot`, `acquireAvailable`, `advanceHead`, `advanceTail` e fechamento).
- O modo estratégico continua priorizando CAS, mas agora limita o número de tentativas antes de dormir e retrocede para o mutex somente após backoff, reduzindo contenção e jitter. O modo assíncrono permanece lock-free, alinhado ao foco de performance.
- `Close` sinaliza `closed = true`, aguarda (com backoff) a drenagem de ocupação e somente então captura o mutex para limpar a memória e cortar o slice, garantindo que novas operações falhem imediatamente sem interromper as que já estavam em andamento.

## Estágio A3 — Extensões Futuras
- A interface `FixedBuffer` agora expõe `Metrics()` e `State()` para que consumidores obtenham instantaneamente ocupação, capacidade e índices atuais sem sincronização adicional. O tipo `Metrics` oferece helper `Utilization()` para leituras percentualizadas.
- Tanto `strategicBuffer` quanto `synchronousBuffer` implementam essas observabilidades usando estruturas já existentes (atomics ou mutex). Não há alocações adicionais — os snapshots são valores por cópia.
- O `StateSnapshot` fornece `Head`/`Tail` atuais para preparar a integração com a futura `Wheel`, permitindo coordenar reuso de slots e políticas de reciclagem sem refatorações profundas.

## Estágio B1 — Estrutura da Wheel
- Criada `internal/wheel` com as interfaces `Expirable` e `Handle`, garantindo que consumidores comuniquem o ciclo de vida dos itens de forma consistente.
- `Wheel` mantém `tickInterval`, `slotCount` (potência de dois para facilitar cálculo modular) e `maxRounds`. As escolhas são aplicadas via opções (`WithTickInterval`, `WithSlotCount`, `WithMaxRounds`) que validam entradas sem alocar memória adicional.
- Cada slot possui ponteiros duplamente ligados (`head`, `tail`) e um `sync.Mutex`; a lista de entradas (`entry`) inclui estado atômico para preparar transições (`scheduled`, `expired`, etc.) quando o loop principal for implementado.
- Preparado `entryPool` lock-free com `atomic.Pointer` para reciclar estruturas de entrada e minimizar pressão na heap quando os agendamentos forem ativados.
- `NewWheel` inicializa canais de parada (`stopCh`, `stopped`) e garante que o intervalo seja ao menos `time.Microsecond`; valores inválidos retornam erros explícitos, preservando previsibilidade.

## Estágio B2 — Loop Principal
- `Start` cria apenas uma goroutine para dirigir a roda usando `time.Ticker`; limites de goroutines seguem a diretriz (1 goroutine interna, demais ficam sob responsabilidade do chamador).
- `Stop` aceita `context.Context`, fecha o ticker e aguarda a goroutine terminar; caso o contexto seja cancelado retorna o erro original do contexto, mantendo previsibilidade de desligamento.
- A posição é avançada com máscara (`slotMask`) derivada de potência de dois, permitindo aritmética sem custos extras; `processSlot` bloqueia o slot e prepara a futura decretação de `roundsLeft`.
- Implementados helpers de slot (`append`, `remove`, `detachAll`) para anexar/remover entradas sem alocação, preparando a próxima fase de agendamento.
- A referência histórica (`.reference/itick/expirador.go`) foi considerada e superada: o novo loop evita mutex global e não cria goroutines adicionais por item, alinhando-se aos requisitos de performance atuais.

## Estágio B3 — API de Agendamento
- `Schedule` resolve ticks e rodadas via `resolvePlacement`, reutiliza `entryPool` e adiciona a entrada ao slot correto com bloqueio mínimo. O cálculo usa `slotMask` para evitar divisão e mantém `roundsLeft` pronto para o loop.
- `Handle` encapsula a entrada e o slot corrente, permitindo `Cancel` (retira e devolve ao pool) e `Reset` (reprograma na roda). Ambos evitam alocação e reutilizam as estruturas existentes.
- `processSlot` decrementa `roundsLeft` e, ao atingir zero, remove a entrada, aciona `Expire()` caso o alvo ainda não tenha expirado e devolve o nó ao pool. Estados (`entryStateScheduled`, `Expired`, `Cancelled`) protegem contra repetição de eventos.
- Limitações atuais: `Reset` não reativa itens já expirados (retorna sucesso silencioso), e o cálculo de `ticks` ainda não contempla saturação em `maxRounds` (abortamos com erro). Esses pontos serão revisitados nos próximos estágios.

## Estágio B4 — Observabilidade e Guardas
- Adicionadas opções `WithExpireHook` e `WithRescheduleHook`, permitindo ligar callbacks leves sem custo adicional quando não configurados. Os hooks são acionados dentro do lock, então devem permanecer rápidos.
- `Schedule`, `Cancel` e `Reset` agora consultam `Running()` para evitar usos após `Stop`, retornando `errNotRunning` ou `false` conforme apropriado.
- Contadores atômicos (`scheduled`, `expired`, `cancelled`, `resetted`) expostos por getters fornecem telemetria rápida sem necessidade de observers externos.
- `expireEntry` e `Reset` atualizam os contadores e invocam hooks apenas após garantir que o alvo ainda está ativo, preservando invariantes sem goroutines extras.

## Estágio B5 — Recursos Avançados
- A opção `WithDeterministicJitterSpan` deriva deslocamentos determinísticos entre `0` e `N` ticks usando um contador atômico e a função `mixDeterministic` (finalizador estilo Murmur) para espalhar bits, evitando dependência de `math/rand` e mantendo zero alocação. O jitter é aplicado somente quando `tickCount > 1`, protegendo timers imediatos.
- `SlotPolicy` expõe explicitamente a política de ordenação (`SlotPolicyInsertionOrder`) e centraliza a decisão em `enqueueEntryWithPolicy`. A lista duplamente ligada e o mutex do slot permanecem, mas a personalização fica contida nessa função para futuras políticas.
- `Handle.KeepAlive()` reutiliza o último timeout armazenado, delegando em `Reset` para recalcular tick/rodadas. Isso facilita o padrão keep-alive sem que consumidores mantenham cópia do intervalo e ainda respeita erros de uso (`errNotRunning`, handle cancelado, etc.).
- `resolvePlacement` agora combina `calculateTickCount` com `applyDeterministicJitter`, garantindo que o jitter seja considerado antes de validar `maxRounds`; qualquer saturação continua sendo reportada como erro, preservando a previsibilidade do Wheel.

## Estágio B6 — Calibração de Jitter
- `CalibrateDeterministicJitter` reutiliza `mixDeterministic` para gerar histogramas alinhados ao comportamento da wheel em produção, garantindo que análises offline reflitam o mesmo espalhamento determinístico aplicado em runtime.
- O `JitterReport` devolve distribuição completa, média e desvio padrão em passagem única; a única alocação consiste no histograma (`maximumAdditionalTicks + 1` posições), mantendo controle de heap.
- O guard clause para `sampleCount == 0` impede calibrações inválidas e simplifica invariantes internas. A função é pura e segura para concorrência, pronta para ser encadeada com observers, ferramentas de telemetria ou CLIs futuros sem refatorações adicionais.
- A execução efetiva das calibrações será conduzida posteriormente, quando parâmetros reais forem validados; manteremos scripts externos que apenas invocam a função para evitar carregar responsabilidades extras na wheel.

## Estágio C1 — Scheduler Proof of Concept
- `Scheduler[T]` orquestra `FixedBuffer` e `Wheel` sem criar goroutines próprias, mantendo o limite rígido imposto ao projeto. O estado global (`schedulerState`) usa `atomic.Uint32` para permitir fechamento idempotente.
- `Acquire` garante que a wheel esteja rodando antes de consumir o item do buffer; em falha de agendamento, o item é devolvido via `releaseValue` que pode acionar `WithReleaseErrorHandler` para observabilidade futura. Isso evita perda de itens mesmo quando a wheel está indisponível.
 - Cada leasing utiliza `leaseBinding[T]` com `atomic.Uint32` + `atomic.Int64` para controlar estado e timeout sem locks; o próprio binding implementa `wheel.Expirable`, garantindo que apenas uma via (manual ou automática) devolva o recurso ao buffer.
- O `Lease` expõe operações de keep-alive (`KeepAlive`) e redefinição (`ResetTimeout`) sobre o handle da wheel, preparando o terreno para políticas e observers nas próximas etapas. O encapsulamento permite adicionar contadores e métricas posteriormente sem quebrar a API.

## Estágio C2 — Otimizações de Alocação
- O `leaseBinding` agora é embutido dentro do próprio `Lease`, permitindo que o agendamento use `&lease.binding` como `wheel.Expirable` e eliminando a necessidade de uma estrutura de expiração separada. Com isso, cada `Acquire` produz uma única alocação (do `Lease`), reduzindo pressão na heap.
- `finalizeRelease` centraliza a devolução ao buffer, removendo duplicidade entre caminhos manual/automático e evitando alocações transitivas ao compartilhar o mesmo código. O estado interno continua protegido por atomics.
- A migração preserva invariantes de sincronização: somente quando `tryRelease` ou `tryExpire` avançam o estado para `Released`/`Expired` é que a devolução ocorre, garantindo que apenas uma via consuma o valor.

## Estágio C3 — Instrumentação Combinada
- `Scheduler.Metrics()` agrupa em um único snapshot os contadores internos (ativos, adquiridos, liberados, expirados, resets, keep-alives) com as métricas do buffer (`buffer.Metrics`) e os contadores públicos da wheel (`WheelMetrics`). O método retorna valores por cópia, mantendo custo O(1).
- As rotinas internas (`onLeaseActivated`, `onLeaseReleased`, `onLeaseExpired`, `onLeaseTimeoutReset`, `onLeaseKeepAlive`) atualizam `atomic.Uint64`, preservando zero locks e zero alocações adicionais durante operações normais.
- A instrumentação permanece opcional: nenhum hook é disparado automaticamente; consumidores só pagam o custo de ler métricas quando chamam `Metrics()`, alinhando-se ao objetivo de não onerar o caminho crítico.
- O exercício `cmd/metricscheck` foi adicionado como ferramenta manual para validar a consistência das métricas combinadas sem acoplar lógica de teste ao pacote; ele respeita o limite de goroutines (somente a wheel roda em background) e serve como referência para cenários futuros de observabilidade.

## Consolidação Final — Tratamento de Erros
- A `Wheel` agora expõe sentinelas descritivas (`ErrInvalidTickInterval`, `ErrWheelAlreadyRunning`, `ErrEntryExpired`, etc.), garantindo mensagens constantes (sem `errors.New` dinâmico) e permitindo que chamadas externas usem `errors.Is` sem depender de variáveis não exportadas.
- Ao revisar as mensagens do `buffer`, todas passaram a ser prefixadas com `buffer:` para manter a identificação do subsistema quando o erro emerge em logs ou supervisionadores.
- O `Scheduler` traduz erros internos da wheel que chegam às leases para as sentinelas públicas (`ErrLeaseExpired`, `ErrHandleUnavailable`), preservando encapsulamento e a semântica já documentada para consumidores diretos do módulo.

## Consolidação Final — Encerramento Determinístico
- `Wheel.Stop` passa a interromper o loop com `running = false` antes de drenar slots, impedindo novas chamadas a `Schedule` durante o desligamento. O dreno (`drainAllSlots`) coleta todas as entradas pendentes, chamando `Expire()` para targets ainda ativos e retornando demais entradas ao pool sem leaks.
- O próprio `Schedule` revalida `running` dentro da região crítica de enfileiramento; se a wheel estiver desligando, o item é descartado e `ErrWheelNotRunning` é propagado imediatamente.
- A estratégia garante que, ao finalizar a wheel, nenhuma entrada permaneça pendente e os recursos associados (ex.: leases do scheduler) sejam devolvidos sem exigir goroutines adicionais.
- `Scheduler.Close` mantém uma lista duplamente ligada de leases ativas protegida por mutex leve; ao encerrar, cancela cada handle em lote e invoca o fluxo de expiração do `leaseBinding`, preservando os contadores e a devolução ao buffer sem depender da wheel continuar rodando.

## Consolidação Final — Benchmarks
- Os benchmarks do `FixedBuffer` operam em linha única e reutilizam os slots pré-carregados para evitar pressão adicional de alocação; servem como baseline para medir regressões nas otimizações de CAS e fallback.
- Para a `Wheel`, os benchmarks exercitam os caminhos de cancelamento e reset sem criar múltiplas goroutines, permitindo observar diretamente o impacto do pool de entradas e da política de slots.
- O benchmark do `Scheduler` combina os componentes reais (buffer + wheel) mantendo o limite de goroutines (apenas a roda em background) e evidencia o custo do ciclo principal `Acquire/Release`, facilitando comparações futuras quando observers ou policies forem adicionados.
- O `Scheduler` passou a usar `leasePool` (`sync.Pool`) para reciclar `Lease`, removendo uma das duas alocações observadas no benchmark; apenas o `scheduledHandle` continua alocando e permanece como pendência controlada para fase posterior.
- O pooling ocorre somente após `leaseBinding.finalizeRelease` encerrar o vínculo com o scheduler e cancelar o handle da wheel, mantendo segurança contra reutilização antecipada.
