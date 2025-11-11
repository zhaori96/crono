# Planejamento do Pacote `crono`

## Estratégia Geral
- **Escopo**: disponibilizar dois blocos principais altamente performáticos — um buffer circular (`FixedBuffer`) e uma time wheel (`Wheel`) — permitindo que aplicações gerenciem itens reutilizáveis e expirações temporais sem criar goroutines por item.
- **Estilo**: código em inglês, nomes completos, sem abreviações arbitrárias; foco em legibilidade estática e funções concisas.
- **Performance**: priorizar custos O(1) amortizados, minimizar alocações na heap por meio de pré-alocação e pools, utilizar `sync/atomic` como primeira linha e `sync.Mutex` apenas como fallback controlado.
- **Legado preservado**: manter padrões já positivos do projeto atual (uso de `NoCopy`, backoff exponencial, opções configuráveis), corrigindo os problemas identificados (ex.: contadores incorretos, retorno `nil` em modo síncrono, wheel incompleta).
- **Incrementalidade**: executar em etapas claras, priorizando entrega de blocos funcionais; testes formais e benchmarks entram após a consolidação das funcionalidades.

## Estágio 0 – Fundamentos Compartilhados
- Preparar repositório independente (`crono`) com `go.mod`, `README` introdutório e seção que enfatize o foco em performance.
- Criar `internal/util/no_copy.go` e outros utilitários necessários (ex.: helpers de atomics) copiando apenas a essência do projeto atual.
- Definir de saída as interfaces públicas:
  - `type Expirable interface { Expire(); Expired() bool }`.
  - `type Handle interface { Cancel() bool; Reset(time.Duration) error }` (será implementado pela wheel).
- Especificar decisões arquiteturais em `docs/architecture.md`, registrando invariantes e justificando uso de atomics ou mutex por componente.
- Criar documento vivo (`docs/progress.md`, por exemplo) para detalhar a lógica de cada funcionalidade e de suas funções públicas/privadas conforme implementadas, atualizando-o a cada estágio concluído.

## Trilha A – `FixedBuffer`

### Estágio A1 – Reescrita Básica
- Reimplementar `FixedBuffer` em inglês (`buffer.go`) mantendo assinatura da interface: `Get`, `Put`, `Release`, `Closed`, `Close`.
- Corrigir erros conhecidos:
  - Ajustar `updateAvailability` para decrementar corretamente (`Add(-1)`).
  - Tratar modo síncrono retornando implementação válida (ex.: bloqueio com condicional) ou erro explícito; nunca retornar `nil`.
  - Garantir que `Release` sempre reflita o número real de itens disponíveis.
- Introduzir enum `AccessMode` (`Synchronous`, `Asynchronous`, `Strategic`) com o mesmo comportamento atual, documentando cada modo.

### Estágio A2 – Afinar Concurrency e Backoff
- Revisar o caminho de fallback: manter backoff exponencial mas limitar ao necessário para reduzir contenção; usar `time.Sleeper` reutilizável para evitar alocação.
- Garantir que `Close` sinalize fechamento antes de limpar o slice, bloqueando novos acessos e protegendo operações em andamento.
- Documentar invariantes dos índices (`head`, `tail`) e testar mentalmente cenários de wrap-around para evitar sobrescrita indevida.

### Estágio A3 – Extensões Futuras
- Planejar integração opcional com métricas leves (`Len`, `Cap`, `Utilization`) usando atomics.
- Manter espaço para acoplar o buffer com a wheel (ver Trilha C), mas sem implementar ainda.

## Trilha B – `Wheel`

### Estágio B1 – Estrutura
- Criar tipo público `Wheel` (`wheel.go`) com campos:
  - `tickInterval time.Duration`
  - `slotCount uint32`
  - `maxRounds uint32`
  - `slots []slot`
  - `position uint32`
  - `running atomic.Bool`
  - `stop chan struct{}`
- Implementar `NewWheel` validando entradas, arredondando intervalos e pré-alocando slots.
- Definir tipo interno `entry` contendo ponteiros duplamente ligados, `target Expirable`, `roundsLeft uint32` e flag de estado.
- Preparar pool (`freeList`) para reutilizar entradas, evitando alocações em `Schedule`.

### Estágio B2 – Loop Principal
- Implementar `Start()` com goroutine única usando `time.NewTicker`.
- Em cada tick:
  - Avançar `position` com aritmética modular.
  - Processar slot corrente:
    - Se `roundsLeft > 0`, decrementar e mover entrada para slot futuro sem alocar.
    - Caso contrário, acionar `Expire()` e devolver `entry` ao `freeList`.
  - Garantir que entradas removidas/zeradas durante o loop não causem race (usar flags atômicas).
- Implementar `Stop(ctx)` garantindo término do loop e reciclagem de todos os recursos.

### Estágio B3 – API de Agendamento
- Expor `Schedule(target Expirable, timeout time.Duration) (*Handle, error)`:
  - Calcular ticks e rodadas restantes (`timeout / tickInterval`).
  - Inserir entrada com o mínimo de locks (um mutex por slot).
  - Retornar `Handle` que referencia internamente a `entry`.
- Implementar `Handle.Reset(timeout)` reutilizando a mesma `entry`, recalculando slot alvo e reencadeando sem alocar.
- Implementar `Handle.Cancel()` cancelando o item em tempo quase O(1), devolvendo `entry` ao pool se bem-sucedido.
- Adaptar `ExpirableContext` (novo nome: `ContextHandle`) permitindo criar `context.Context` cancelado automaticamente pela wheel.

### Estágio B4 – Otimizações e Observabilidade
- Adicionar contadores atômicos (`scheduled`, `expired`, `cancelled`, `rescheduled`) com getters opcionais.
- Oferecer hooks (`OnExpire`, `OnReschedule`) configuráveis via `WheelOptions`, aplicados apenas quando fornecidos.
- Proteger `Start` contra chamadas repetidas e `Schedule` contra uso após `Stop`.

### Estágio B5 – Recursos Avançados
- Implementar jitter determinístico opcional para evitar thundering herd.
- Adicionar suporte a políticas FIFO por slot (processar entradas na ordem de inserção) usando lista encadeada.
- Considerar mecanismo de “auto-reset” on-use: expor método público para consumidores indicarem atividade e renovarem timer.

### Estágio B6 – Ferramentas de Calibração
- Disponibilizar utilitário puramente determinístico que reproduza a distribuição do jitter aplicado em produção (`CalibrateDeterministicJitter`), gerando histogramas e estatísticas sem alocações excessivas.
- Manter a função em `internal/wheel` para consumo por ferramentas externas (CLIs, observers) sem introduzir dependências no caminho crítico da wheel.

## Trilha C – Integração Buffer + Wheel
- Planejar módulo `scheduler` que combine `FixedBuffer` e `Wheel`:
  - Consumidores fazem `Put` no buffer; a wheel monitora inatividade e libera (ou recicla) itens automaticamente.
  - Permitir configurar política de remoção (descartar, callback, reenqueue).
- Estágios:
  - C1: prova de conceito com combinações simples.
  - C2: otimizações para evitar alocações cruzadas (reutilizar `leaseBinding` embutido, caminho único de devolução e contadores atômicos sem buffers auxiliares).
  - C3: instrumentação opcional (métricas combinadas `Scheduler.Metrics` expondo contadores internos, `buffer.Metrics` e estatísticas da wheel; utilitário `cmd/metricscheck` demonstra o cenário base e pode evoluir para ferramenta de inspeção).

## Consolidação Final (após trilhas A/B/C)
- Revisar manuseio de erros em toda a API.
- Validar que `Stop`/`Close` liberam recursos e bloqueiam novas operações.
- Somente após as trilhas principais iniciar planejamento de testes e benchmarks oficiais.

## Itens Definidos porém Pendentes
- Reset eficiente de entradas na wheel (Trilha B3) — ainda sem referência pronta, precisa ser projetado.
- Pool (`freeList`) deve suportar concorrência multi-core sem gerar garbage.
- Callbacks e jitter (Trilha B5) previstos, mas aguardando implementação.
- Integração buffer + wheel (Trilha C) depende das trilhas iniciais.

---

## Features Futuras Planejadas (fora do roadmap imediato)
- Suporte a múltiplas wheels shardadas, coordenadas externamente, para workloads massivos.
- Sistema de telemetria plugável (observer) para expor métricas e latências sem dependências externas.
- Compatibilidade opcional com relógio lógico/virtual para simulações ou ambientes determinísticos.
- Classes de prioridade (ex.: `High`, `Normal`, `Low`) mapeadas para wheels distintas administradas pelo mesmo coordenador.
- Rate limiter baseado no wheel, reutilizando ticks para emissão de tokens com custo mínimo.
- API de inspeção (hooks ou endpoints) que liste slots ativos, próximos vencimentos e estatísticas de reset/cancelamento.
- Plugins de política configuráveis (`OnExpire`, `OnReschedule`, `OnCancel`) permitindo alterar comportamento sem tocar na base.
- Integração facilitada com pools de conexão/cache via helpers que unem `FixedBuffer` e `Wheel`.
- Modo auditável leve registrando eventos (schedule/reset/expire) para diagnósticos.
- Ferramentas de benchmark/CLI que simulam cargas intensas para calibração de parâmetros.
- Evoluir `cmd/metricscheck` para um CLI parametrizável (flags, export em JSON) que facilite inspeções sem alterar código-fonte.

> **Nota estratégica**: a arquitetura deve prever interfaces e pontos de extensão para todos os itens acima desde os estágios iniciais (opções, observers, policies), de forma que a implementação futura não exija refatorações profundas — idealmente nenhuma refatoração, apenas adição de código.
