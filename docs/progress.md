# Progresso do Desenvolvimento – `crono`

## Estágio 0 — Fundamentos do Repositório
- Módulo Go inicializado como `github.com/leonardo/crono`.
- Documentos de planejamento (`CRONO.md`, `AGENTS.md`) consolidados para orientar os próximos estágios.
- Estrutura de documentação criada para registrar decisões arquiteturais (`docs/architecture.md`) e anotações de implementação (`docs/progress.md`).

## Estágio A1 — Reescrita do FixedBuffer
- Status: concluído.
- Implementado `internal/buffer` com interface `FixedBuffer[T]` (`Get`, `Put`, `Release`, `Closed`, `Close`) e enum `AccessMode` (`Synchronous`, `Asynchronous`, `Strategic`).
- `strategicBuffer` reutiliza slices pré-alocados, controla ocupação com atomics e aplica fallback com mutex e backoff exponencial apenas quando permitido (modo estratégico). `AccessModeAsynchronous` usa exclusivamente atomics.
- `Release` agora recebe o item devolvido; ao detectar inconsistências (ex.: devolução excedendo a capacidade) retorna `ErrReuseMismatch`.
- `synchronousBuffer` usa `sync.Mutex` + `sync.Cond` para bloquear leitores/escritores de forma determinística, evitando retornos nulos no modo síncrono.

## Estágio A2 — Ajustes de Concorrência
- Status: concluído.
- Criado `internal/util.ExponentialSleeper` para manter backoff exponencial sem alocar timers a cada tentativa.
- `strategicBuffer` aplica o sleeper em todas as rotinas que podem recuar para o mutex, limitando a quantidade de CAS antes de dormir e reduzir contenção.
- `Close` aguarda a ocupação zerar com backoff progressivo e, então, limpa o slice sob mutex, garantindo que novos acessos falhem enquanto operações pendentes finalizam com segurança.

## Estágio A3 — Extensões Futuras
- Status: concluído.
- A interface `FixedBuffer` ganhou `Metrics()` e `State()`, suportadas pelos tipos `Metrics` e `StateSnapshot` para acesso O(1) a capacidade, ocupação e ponteiros do buffer.
- `strategicBuffer` deriva os valores por atomics, enquanto `synchronousBuffer` usa seu mutex interno, mantendo zero alocação adicional e preservando a coerência.
- A nova API fornece a base para integrar métricas leves e coordenar futuras interações com a `Wheel` sem reformulações profundas.

## Estágio B1 — Estrutura da Wheel
- Status: concluído.
- Implementada `internal/wheel` com `Expirable`/`Handle`, opções configuráveis e validações que normalizam intervalos e contagem de slots para suportar cálculos atômicos.
- Definidos `Wheel`, `slot`, `entry` e `entryPool`, todos pré-alocados e preparados para o loop principal do agendador sem introduzir alocações durante operação normal.
- `entryPool` utiliza `atomic.Pointer` como lista livre lock-free, antecipando alto volume de reuso quando o agendamento estiver ativo.

## Estágio B2 — Loop Principal
- Status: concluído.
- `Start` ativa a roda com apenas uma goroutine baseada em `time.Ticker`, obedecendo ao limite rígido de goroutines internas.
- `Stop` usa `context.Context`, sinaliza o loop e aguarda a conclusão; erros derivados do contexto são propagados para diagnóstico.
- O avanço da posição usa máscara (`slotMask`) garantindo wrap-around sem divisão; `processSlot` mantém o slot trancado apenas durante a preparação da futura decretação de `roundsLeft`.
- Helpers (`append`, `remove`, `detachAll`) foram implementados nos slots para suportar inserção e remoção de entradas no próximo estágio sem custo adicional de alocação.

## Estágio B3 — API de Agendamento
- Status: concluído.
- `Schedule` instancia entradas via `entryPool`, calcula ticks/rounds com `resolvePlacement` e adiciona ao slot correspondente; falhas por `timeout` negativo ou exceder `maxRounds` retornam erro imediato.
- `Handle.Cancel` remove a entrada e devolve ao pool, retornando `false` apenas se o item já estava expirado ou cancelado. `Handle.Reset` reposiciona a entrada respeitando o slot corrente e atualiza `roundsLeft` sem realocar.
- `processSlot` agora remove entradas com `roundsLeft == 0`, chama `Expire()` e reutiliza o nó; estados protegem contra expiração dupla e sustentam o fluxo de cancelamento/reset.

## Estágio B4 — Observabilidade e Guardas
- Status: concluído.
- Incluídos hooks opcionais (`WithExpireHook`, `WithRescheduleHook`) ativos somente quando configurados, mantendo overhead zero no caminho crítico em uso padrão.
- `Schedule`, `Cancel` e `Reset` passaram a verificar `Running()` para impedir operações após `Stop`, retornando `errNotRunning`/`false` quando necessário.
- Adicionados contadores atômicos (`ScheduledCount`, `ExpiredCount`, `CancelledCount`, `RescheduledCount`) para telemetria leve sem dependências externas.
- Hooks e contadores são atualizados dentro das regiões críticas para preservar consistência, mantendo o limite de goroutines internas inalterado.

## Estágio B5 — Recursos Avançados
- Status: concluído.
- Opção `WithDeterministicJitterSpan` introduzida para adicionar jitter determinístico até `N` ticks extras, calculado via contador atômico e função de espalhamento sem alocações; o jitter só é aplicado quando o timeout cobre pelo menos dois ticks, preservando timers imediatos.
- Criado `SlotPolicy` com política padrão `SlotPolicyInsertionOrder`, garantindo FIFO explícito por slot e preparando caminho para políticas futuras; o enfileiramento passa por `enqueueEntryWithPolicy` para facilitar extensões.
- `Handle` ganhou `KeepAlive()` que reaproveita o último timeout fornecido, permitindo renovar o timer quando o recurso é utilizado sem recalcular durações; a implementação reutiliza `Reset` e respeita o estado atual da wheel.
- `resolvePlacement` agora considera jitter antes de calcular `roundsLeft`, assegurando que o limite `maxRounds` continue válido mesmo com o deslocamento adicional.

## Estágio B6 — Calibração de Jitter
- Status: concluído.
- Disponibilizado `CalibrateDeterministicJitter` em `internal/wheel` para gerar histogramas determinísticos a partir do mesmo `mixDeterministic` usado no runtime, evitando divergências entre calibração e operação real.
- O relatório (`JitterReport`) retorna distribuição completa, média e desvio padrão com alocação única proporcional ao span (`maximumAdditionalTicks + 1`), preservando o foco em baixa alocação.
- Erro explícito para `sampleCount == 0` garante que chamadores forneçam tamanho de amostra significativo; a função permanece livre de sincronização e adequada para ferramentas/offline usage.
- Pendência registrada: executar calibrações práticas (ex.: `go run ./cmd/calibrator`) quando os parâmetros finais de jitter forem definidos, garantindo que as escolhas reflitam workloads reais.

## Estágio C1 — Prova de Conceito do Scheduler
- Status: concluído.
- Criado `internal/scheduler` com `Scheduler[T]` que combina `FixedBuffer` e `Wheel`, respeitando a regra de zero goroutines adicionais além da própria wheel; `Scheduler.Close` apenas sinaliza encerramento e propaga `Close` ao buffer.
- `Acquire` e `AcquireWithTimeout` obtêm itens do buffer e registram expiração automática na wheel; caso a programação falhe, o item é devolvido imediatamente ao buffer para evitar vazamentos.
- O tipo `Lease[T]` encapsula o item, expiração (`wheel.Handle`) e métodos de controle (`Value`, `Release`, `ResetTimeout`, `KeepAlive`, `Timeout`). A expiração automática reutiliza o mesmo caminho de `Release`, evitando duplicação e garantindo que apenas uma devolução ocorra.
- `WithIdleTimeout` define o tempo padrão de ociosidade; `WithReleaseErrorHandler` permite capturar falhas do buffer durante liberações automáticas, preparando terreno para políticas mais sofisticadas nos estágios seguintes.

## Estágio C2 — Otimizações de Alocação
- Status: concluído.
- O `Lease` passou a conter o `leaseBinding` embutido, permitindo que a wheel agende o próprio binding sem gerar estruturas auxiliares (`leaseExpiration`). Cada aquisição agora realiza apenas uma alocação no caminho quente.
- `leaseBinding` cuida da liberação manual e da expiração automática usando o mesmo fluxo (`finalizeRelease`), evitando duplicidades e garantindo que o item seja devolvido ao buffer sem ressurgir alocações transitivas.
- As contagens internas (`onLeaseActivated`, `onLeaseReleased`, `onLeaseExpired`) usam atomics em vez de estruturas extras, mantendo o custo constante e pronto para extensões futuras.

## Estágio C3 — Instrumentação Combinada
- Status: concluído.
- Introduzido `Scheduler.Metrics()` que expõe métricas de uso (ativos, adquiridos, liberados, expirados, resets, keep-alives) ao lado de `buffer.Metrics` e contadores da wheel (`WheelMetrics`), fornecendo um panorama unificado.
- Operações relevantes (`Activate`, `Release`, `Expire`, `ResetTimeout`, `KeepAlive`) atualizam contadores atômicos dedicados, mantendo o custo zero quando o consumidor não coleta métricas.
- O design mantém a instrumentação opcional: quem não invocar `Metrics()` não paga custo adicional, e as estruturas retornadas são simples snapshots por valor, sem alocações posteriores.
- Criado utilitário `cmd/metricscheck` que executa um cenário determinístico (acquire, keep-alive, reset, release, expiração) e imprime o snapshot de métricas, validando o comportamento das contagens combinadas.
- Pendência futura: transformar `cmd/metricscheck` em um CLI parametrizável (flags, JSON) para facilitar inspeções sem alterar código durante diagnósticos.

## Consolidação Final — Revisão de Erros
- Status: concluído.
- Padronizados todos os erros públicos da wheel com sentinelas nomeadas (`ErrInvalidTickInterval`, `ErrNegativeTimeout`, `ErrEntryExpired`, etc.), evitando alocações repetidas e permitindo inspeção via `errors.Is`.
- `buffer` passou a prefixar as mensagens com `buffer:` para manter consistência com os demais pacotes internos.
- `Lease.ResetTimeout` e `Lease.KeepAlive` convertem `wheel.ErrEntryExpired` para `scheduler.ErrLeaseExpired`, alinhando o contrato público do scheduler.

## Consolidação Final — Encerramento Determinístico
- Status: concluído.
- `Wheel.Stop` agora marca a roda como inativa antes de encerrar o loop principal e drena todos os slots, expirando alvos ainda pendentes para que recursos sejam devolvidos imediatamente.
- `Schedule` realiza uma verificação adicional sob o lock do slot, evitando que novos itens sejam programados enquanto o desligamento está em andamento.
- O dreno diferencia entradas ativas (`entryStateScheduled`) de itens já cancelados, reaproveitando o pool sem gerar expirations duplicadas.
- `Scheduler.Close` recolhe todas as leases ainda registradas, cancela os handles ativos e força a expiração via `leaseBinding`, garantindo que cada recurso retorne ao buffer mesmo que o consumidor não invoque `Release`.

## Consolidação Final — Benchmarks de Referência
- Status: concluído.
- Adicionados benchmarks para o `FixedBuffer` nos modos estratégico e síncrono, medindo o ciclo `Get`/`Release` sem goroutines adicionais.
- A `Wheel` recebeu benchmarks separados para `Schedule + Cancel` e para `Handle.Reset`, reutilizando expiráveis pré-alocados e mantendo a roda ativa com uma única goroutine.
- O `Scheduler` agora possui benchmark `Acquire + Release`, validando o fluxo completo com buffer pré-carregado, wheel ativa e encerramento controlado via `Cleanup`.
- Após introduzir o pool de `Lease`, `BenchmarkSchedulerAcquireRelease` passou para 1 alocação/op (≈32 B). A única alocação remanescente vem do `scheduledHandle` da wheel, marcado para otimização futura.

## Consolidação Final — Pool de Lease
- Status: concluído.
- O `Scheduler` agora mantém `leasePool` interno (`sync.Pool`) e recicla instâncias de `Lease` no fluxo de aquisição/expiração/liberação.
- `leaseBinding.finalizeRelease` devolve automaticamente a lease ao pool após devolver o valor ao buffer, mantendo invariantes de métricas e a lista de ativos.
- Operações de shutdown (`Close`, `releaseOnSchedulerClose`) continuam válidas: o pooling ocorre somente após a lease sair da lista de ativos e cancelar o handle da wheel.
