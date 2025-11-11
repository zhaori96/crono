# Crono – Decomposição Completa de Lógica e Intenções

Este documento destrincha cada parte do projeto `crono`, explicando **a motivação**, **as invariantes** e **o funcionamento interno** de todos os tipos, funções e fluxos públicos ou privados. A intenção é que, após esta leitura, você consiga navegar no código com a mesma clareza de quem o implementou.

---

## 1. Visão Macro do Runtime

- **Goroutines internas**: apenas a `Wheel` mantém uma goroutine (loop do ticker). O `FixedBuffer` e o `Scheduler` são completamente síncronos. O limite total imposto é ≤ 5 goroutines; a implementação atual usa 1.
- **Fluxo principal**:
  1. Itens são armazenados no `FixedBuffer` (modo configurável de sincronização).
  2. `Scheduler.Acquire()` retira um item do buffer e agenda sua expiração na `Wheel`.
  3. O consumidor opera sobre a `Lease`.
  4. A devolução ocorre manualmente (`Lease.Release`) ou automaticamente (`Wheel` expira e invoca `leaseBinding.Expire()`).
- **Diretrizes refletidas**: nomes completos, zero abreviações, utilização de atomics, pooling agressivo, documentação contínua (`docs/architecture.md`, `docs/progress.md`, este arquivo) e preparo para extensões futuras (hooks, policies, observabilidade).

---

## 2. Pacote `internal/util`

### 2.1 `NoCopy` (`internal/util/no_copy.go`)
- **Objetivo**: impedir cópias acidentais de structs que possuem estado compartilhado não seguro para cópia (ex.: locks, atomics).
- **Funcionamento**: inserir `_ util.NoCopy` em um struct faz com que `go vet -copylocks` acuse qualquer atribuição por cópia, porque os métodos `Lock`/`Unlock` (ainda que vazios) sinalizam a presença de um lock lógico.
- **Uso no projeto**: aplicado em `Wheel`, `strategicBuffer`, `synchronousBuffer` e `Scheduler`, reforçando que todos são utilizados por ponteiro.

### 2.2 `ExponentialSleeper` (`internal/util/sleeper.go`)
- **Motivação**: a versão original do código criava timers a cada tentativa de backoff. Aqui encapsulamos uma sequência de `time.Sleep` exponencial sem alocações.
- **Campos**:
  - `current`: duração atual a ser aguardada.
  - `maximum`: teto para o backoff.
- **`NewExponentialSleeper(start, maximum)`**:
  - Normaliza `start` (mínimo 1 µs) e garante `maximum ≥ start`.
  - Retorna o sleeper por valor, permitindo alocação em stack.
- **`Pause()`**:
  - Executa `time.Sleep(current)`.
  - Dobra `current`; se ultrapassar `maximum`, prende no teto.
- **Uso**: todos os caminhos de fallback do `strategicBuffer` compartilham a mesma semântica de backoff, reduzindo jitter ao ceder o lock.

---

## 3. Pacote `internal/buffer`

### 3.1 Contrato Público
- **Interface `FixedBuffer[T]` (`internal/buffer/fixed_buffer.go`)**:
  - `Get`, `Put`, `Release`, `Closed`, `Close`, `Metrics`, `State`.
  - O contrato exige que `Release` devolva um item obtido e que `Close` seja idempotente.
- **`AccessMode` (`internal/buffer/mode.go`)**:
  - `Synchronous`: usa apenas `sync.Mutex` + `sync.Cond`.
  - `Asynchronous`: apenas atomics, sem fallback.
  - `Strategic`: atomics + fallback controlado (backoff + `fallbackMux`).
  - `String()` serve para logs/configuração.
- **Erros (`internal/buffer/errors.go`)**:
  - Sentinelas descrevendo todas as condições esperadas: vazio, cheio, fechado, modo inválido, capacidade inválida, violação de reuse.

### 3.2 Invariantes globais
1. `0 ≤ count/occupancy ≤ capacity`.
2. `head` e `tail` sempre avançam com `(index + 1) % capacity`.
3. Depois que `Close` é acionado:
   - `Closed() == true`.
   - Nenhum novo item pode ser inserido.
   - Slots são zerados e o slice é liberado da memória.

### 3.3 Implementação Síncrona (`internal/buffer/synchronous.go`)

#### Estrutura
- `items []T`: armazena o conteúdo real.
- `capacity`, `head`, `tail`, `count`: números inteiros protegidos pelo `mutex`.
- `closed`: evita reuso após `Close`.
- `notEmpty` e `notFull`: condições para bloquear leitores e escritores, respectivamente.

#### Métodos
- `newSynchronousBuffer(capacity)`:
  - Aloca slice fixo.
  - Instancia as duas `sync.Cond` sobre o mesmo mutex.
- `Get()`:
  1. Trava `mutex`.
  2. Enquanto `count == 0`, espera em `notEmpty`. Se `closed` for sinalizado no meio da espera, aborta com `ErrBufferClosed`.
  3. Avança `tail`, copia o item, zera a posição (evitando retenção de referências) e decrementa `count`.
  4. `Signal` em `notFull` acorda produtores bloqueados.
- `Put(item)` / `Release(item)`:
  - Ambos chamam `enqueue(item)`:
    1. Trava `mutex`.
    2. Enquanto `count == capacity`, aguarda `notFull`. Respeita `closed`.
    3. Avança `head`, grava o item, incrementa `count`.
    4. `Signal` em `notEmpty`.
  - `Release` compartilha o mesmo caminho de `Put` para manter lógica uniforme.
- `Metrics()` / `State()`:
  - Capturam um snapshot sob lock; retornam estruturas por valor, sem alocar.
- `Close()`:
  1. Garante idempotência via `if b.closed { return }`.
  2. Define `closed = true`.
  3. Itera zerando `items` e depois `Clip` + `nil` para liberar memória.
  4. `Broadcast` acorda todas as goroutines pendentes.

### 3.4 Implementação Estratégica (`internal/buffer/strategic.go`)

#### Estrutura
- `items []T`, `capacity uint32`: memória fixa.
- Atomics:
  - `head`, `tail` (`atomic.Uint32`) para índices circulares.
  - `occupancy` (`atomic.Int32`) para contagem.
  - `closed` (`atomic.Bool`) para sinalização.
- `allowLock bool`: habilita fallback (modo `Strategic`).
- `fallbackMux sync.Mutex`: usado apenas para recuperar consistência depois de várias disputas.

#### Pipeline de `Put`/`Release`
1. Checar `closed`. Operações após o fechamento são rejeitadas.
2. `reserveSlot()`:
   - **Sem fallback** (`allowLock = false`):
     - Loop CAS até sucesso ou lotação (`current == capacity`).
   - **Com fallback**:
     - Tenta até 10 vezes com CAS + `ExponentialSleeper`.
     - Se falhar, trava `fallbackMux`, revalida `occupancy` e incrementa manualmente.
3. `advanceHead()`:
   - Loop CAS semelhante ao anterior.
   - Em caso de fallback, o lock garante que o índice não seja corrompido.
4. Escrever item no índice retornado.
5. Se `closed` tiver sido sinalizado entre reserva e escrita:
   - `rollbackReservation()` decrementa `occupancy`.
   - `restoreHead(previous, current)` desfaz o avanço de `head`.
6. Para `Release`, um `ErrBufferFull` é traduzido para `ErrReuseMismatch`, sinalizando bug no chamador (devolução excedendo capacidade).

#### Pipeline de `Get`
1. Se `closed && occupancy == 0`, retorna `ErrBufferClosed`.
2. `acquireAvailable()`:
   - Mesma estratégia de `reserveSlot`, porém decrementando `occupancy`.
   - Retorna `false` se vazio (permitindo `ErrBufferEmpty`).
3. `advanceTail()` encontra a posição do item.
4. Copia o valor, zera o slot e devolve.

#### `Close()`
- Usa `CompareAndSwap(false, true)` para evitar repetições.
- Aguarda até 64 tentativas que `occupancy` zere (com backoff) antes de segurar o mutex.
- Dentro do lock, zera `occupancy`, limpa itens, `Clip` e `nil` no slice.

#### Funções auxiliares e intenções
- `reserveSlot`, `acquireAvailable`: isolam manipulação de contadores, deixando claro que `Get` e `Put` compartilham o mesmo caminho.
- `rollbackReservation`, `restoreHead`: garantem atomicidade lógica mesmo sem locks permanentes.
- `Closed()`/`Metrics()`/`State()`: leitura direta dos atomics, custo O(1).

### 3.5 Integração futura
- O buffer foi projetado para interagir com a `Wheel` através do `Scheduler`. `Metrics` e `State` já foram pensados para alimentar features de inspeção, telemetria e políticas que ainda serão adicionadas.

---

## 4. Pacote `internal/wheel`

### 4.1 Objetivo
- Substituir “várias goroutines por timer” por um loop único com custo determinístico.
- Agendar `Expirable`s que serão disparados após um determinado número de ticks, com suporte a reset/cancel.
- Evitar alocações constantes: entradas são reaproveitadas via pool lock-free.

### 4.2 Estruturas principais (`internal/wheel/wheel.go`)
- `Wheel`:
  - Configuração: `tickInterval`, `slotCount`, `maxRounds`, `slotMask`, `slotPolicy`, `deterministicJitterMax`.
  - Estado: `slots []wheelSlot`, `position atomic.Uint32`, `running atomic.Bool`.
  - Sincronização: `control sync.Mutex` (Start/Stop), `stopSignal`, `stoppedSignal`.
  - Observabilidade: callbacks (`onExpireCallback`, `onRescheduleCallback`) e contadores (`scheduledCount`, `expiredCount`, `cancelledCount`, `rescheduledCount`).
  - Pool: `entryPool wheelEntryPool`.
- `wheelSlot`:
  - `headEntry`/`tailEntry`: ponteiros para a lista duplamente ligada.
  - `mutex`: protege manipulações dentro do slot.
- `wheelEntry`:
  - Encapsula `target Expirable`, `roundsLeft`, ponteiros `next/previous`, e `state`.
  - Embute `util.NoCopy` para evitar cópias.
- `scheduledHandle`:
  - Ponteiros para wheel, entry e slot; armazena `deadline` (último timeout).
  - Entregue ao usuário para cancelamento/reset/keep-alive.
- `wheelEntryPool`:
  - `atomic.Pointer[wheelEntry]` implementa freelist LIFO sem locks.

### 4.3 Inicialização (`NewWheel`)
1. Aplica `defaultConfiguration` (tick 10 ms, 64 slots).
2. Executa cada `Option`:
   - `WithTickInterval`, `WithSlotCount`, `WithMaxRounds`, `WithExpireHook`, `WithRescheduleHook`, `WithDeterministicJitterSpan`, `WithSlotPolicy`.
3. Valida parâmetros (tick > 0, slots > 0, maxRounds > 0).
4. `normalizeSlotCount` converte para potência de dois (facilita máscara).
5. Normaliza `tickInterval` (mínimo 1 µs).
6. Instancia a `Wheel` com os slots já alocados; nenhuma goroutine é criada nesse momento.

### 4.4 Ciclo de vida (Start/Stop)
- **`Start()`**:
  - Protegido por `control`.
  - Se `running` já estiver `true`, retorna `ErrWheelAlreadyRunning`.
  - Reseta `position`, cria `stopSignal`/`stoppedSignal`, configura `ticker`.
  - Marca `running = true` e dispara `go w.run(...)`.
- **`run(ticker, stop, stopped)`**:
  - `defer`: para o ticker, marca `running=false`, chama `drainAllSlots()` e fecha `stopped`.
  - Loop: `select` entre `stop` (encerra) e `ticker.C` (processa tick).
- **`Stop(ctx)`**:
  - Se `ctx == nil`, usa `context.Background()`.
  - Usa `control` para ler canais com segurança; se a roda não estiver rodando, `ErrWheelNotRunning`.
  - Fecha `stopSignal` e aguarda `stopped` ou `ctx.Done()`.
  - Ao finalizar, toda a memória de entries é devolvida ao pool ou expirada.

### 4.5 Processamento de tick
- **`processTick()`**:
  - `advancePosition()` incrementa a posição com CAS: `(current + 1) & slotMask`.
  - `processSlot(slotIndex)` percorre a lista daquele slot.
- **`processSlot(slotIndex)`**:
  - Trava o `mutex` do slot.
  - Para cada entry:
    - Se `roundsLeft > 0`, apenas decrementa.
    - Se `roundsLeft == 0`, remove da lista (`removeEntry`) e chama `expireEntry`.
  - A lista é percorrida usando um ponteiro `nextEntry` salvo previamente para evitar perder o encadeamento após a remoção.
- **`expireEntry(entry)`**:
  - Garante que o estado ainda é `entryStateScheduled` (cancelamentos/reset trocam o estado).
  - Marca `state=entryStateExpired`, captura `target`, zera referência.
  - Testa `target.Expired()`; se ainda ativo, chama `target.Expire()`.
  - Executa `onExpireCallback` se configurado.
  1. Incrementa `expiredCount` e retorna entry ao pool.

### 4.6 Agendamento e handles
- **`Schedule(target, timeout)`**:
  1. Valida `target` e `timeout` (não nulo/negativo).
  2. Exige `Running() == true`; evita agendamentos quando a roda está parada.
  3. Calcula `(offset, rounds)` via `resolvePlacement(timeout)`:
     - `calculateTickCount` arredonda para cima (mínimo 1 tick).
     - `applyDeterministicJitter` soma jitter quando configurado.
  4. Verifica se `rounds > maxRounds` (proteger overflow lógico).
  5. Obtém `wheelEntry` do `entryPool`.
  6. Identifica `wheelSlotIndex = (Position() + offset) & slotMask`.
  7. Trava o slot, revalida `running` para detectar `Stop` concorrente, insere a entry respeitando a política (FIFO).
  8. Incrementa `scheduledCount` e devolve `*scheduledHandle`.
- **`scheduledHandle.Cancel()`**:
  - Retorna `false` se handle/entry inválido ou se a wheel não estiver rodando.
  - Trava o slot, checa `entry.state`.
  - Se ainda `Scheduled`, remove, marca `Cancelled`, devolve ao pool, incrementa `cancelledCount`.
  - Se já `Expired`, retorna `false` (nada a cancelar).
- **`scheduledHandle.Reset(timeout)`**:
  1. Rejeita handle inativo ou `timeout < 0`.
  2. Exige wheel rodando.
  3. Trava o slot atual, remove entry e captura estado.
     - `Expired` → `ErrEntryExpired`.
     - Qualquer estado diferente de `Scheduled` → `ErrEntryNotScheduled`.
  4. Calcula novo `(offset, rounds)`; verifica `maxRounds`.
  5. Insere entry no novo slot, atualiza `roundsLeft` e `state`.
  6. Atualiza ponteiro `slotReference`, `deadline` e incrementa `rescheduledCount`.
  7. Dispara `onRescheduleCallback` se configurado.
- **`scheduledHandle.KeepAlive()`** reutiliza `deadline` chamando `Reset(deadline)`.

### 4.7 Deterministic Jitter
- **Configuração**: `WithDeterministicJitterSpan(N)` permite deslocamentos de `0` a `N` ticks.
- **`applyDeterministicJitter`**:
  - Usa incremento atômico (`jitterCounter`) como entrada.
  - `mixDeterministic` (finalizador Murmur) dá boa distribuição sem PRNG adicional.
  - Protege contra overflow usando `math.MaxUint64`.
- **Ferramenta de calibração (`internal/wheel/jitter_calibration.go`)**:
  - `CalibrateDeterministicJitter(maxTicks, sampleCount)`:
    - Reproduz a mesma função determinística para gerar histograma, média e desvio padrão.
    - `sampleCount` precisa ser > 0 (`errInvalidCalibrationSampleCount`).

### 4.8 Dreno seguro
- **`drainAllSlots()`**:
  - Durante `Stop`, cada slot é travado, sua lista é destacada e processada fora do lock.
  - Entradas ainda `Scheduled` sofrem `expireEntry`.
  - Entradas em outros estados têm campos zerados e vão para o pool.
- **Garantia**: ao terminar `Stop`, nenhuma entry fica pendurada e todos os `Expirable` recebem seu `Expire()` quando necessário.

---

## 5. Pacote `internal/scheduler`

### 5.1 Papel geral
- Conectar `FixedBuffer` e `Wheel` para fornecer `Lease`s reutilizáveis com expiração automática.
- Não cria goroutines próprias; depende de a wheel já estar `Start()`.
- Mantém métricas, pooling e lista de ativos para suportar fechamento limpo e futuras inspeções.

### 5.2 Estrutura `Scheduler[T]` (`internal/scheduler/scheduler.go`)
- Campos relevantes:
  - `resourceBuffer`: instância de `FixedBuffer[T]`.
  - `timeWheel`: ponteiro para `Wheel`.
  - `idleTimeout`: duração padrão para novas leases.
  - `releaseErrorHandler`: callback opcional para lidar com falhas do buffer.
  - Contadores atômicos (`activeLeases`, `acquiredCount`, `releasedCount`, `expiredCount`, `resetCount`, `keepAliveCount`).
  - `state`: indica ativo x fechado.
  - `registryMutex` + `activeLeasesHead`: lista duplamente ligada de leases em uso.
  - `leasePool`: `sync.Pool` com instâncias reutilizáveis de `Lease[T]`.
- `NewScheduler(buffer, wheel, options...)`:
  - Valida argumentos não nulos.
  - Aplica opções (`WithIdleTimeout`, `WithReleaseErrorHandler`).
  - Exige `idleTimeout > 0`.
  - Retorna scheduler sem side effects (nem goroutines, nem agendamentos).

### 5.3 Máquina de estados de `Lease`

| Estado              | Significado                                         | Próximos estados válidos           |
|---------------------|-----------------------------------------------------|------------------------------------|
| `leaseStatePending` | Lease recém-criada, ainda não registrada            | `Active`, `Expired` (no fechamento)|
| `leaseStateActive`  | Lease entregue ao cliente e com handle válido       | `Released`, `Expired`              |
| `leaseStateReleased`| Devolução manual concluída                          | —                                  |
| `leaseStateExpired` | Expiração automática ou fechamento forçado          | —                                  |

- Transições são controladas por `leaseBinding.tryRelease()` e `leaseBinding.tryExpire()` (ambas CAS).
- `finalizeRelease(manual bool)` consolida limpeza, devolução do valor ao buffer e atualização de métricas.

### 5.4 Fluxo `Acquire`
1. Verifica `Closed()` → `ErrSchedulerClosed`.
2. Exige `timeWheel.Running()` → `ErrWheelNotRunning` se não.
3. `resourceBuffer.Get()` busca um item; falhas são propagadas.
4. `lease := leasePool.acquire()` reaproveita ou instancia `Lease`.
5. `lease.binding.initialize(lease, scheduler, value, idleTimeout)`:
   - Estado `Pending`.
   - Armazena valor e timeout.
6. `handle, scheduleErr := timeWheel.Schedule(&lease.binding, idleTimeout)`:
   - `lease.binding` implementa `wheel.Expirable` (`Expire` e `Expired`).
   - Em erro: devolve lease ao pool, item ao buffer e retorna o erro.
7. `lease.storeHandle(handle)` guarda o handle usando `handleMutex`.
8. `lease.binding.activate()`:
   - `registerActiveLease(lease)` insere na lista.
   - Estado `Active`.
   - Contadores `acquiredCount` e `activeLeases` são incrementados.
9. Retorna `lease`.

### 5.5 Fluxo `Lease.Release`
1. Verifica `lease != nil` e estado `Active`.
2. `binding.tryRelease()` realiza CAS (`Active` → `Released`).
3. Se o estado já estava `Expired`, retorna `ErrLeaseExpired`; se não era mais ativo, `ErrLeaseInactive`.
4. `takeHandle()` remove o handle atual e chama `Cancel()` (evita expiração tardia).
5. `binding.finalizeRelease(true)`:
   - `unregisterActiveLease`.
   - `consumeValue` zera o item local.
   - `scheduler.releaseValue(value)` devolve ao buffer; se houver erro, `releaseErrorHandler` é invocado.
   - `scheduler.onLeaseReleased()` incrementa `releasedCount` e reduz `activeLeases`.
   - Lease vai para o pool (`scheduler.leasePool.release`).

### 5.6 Fluxo de Expiração Automática
1. A `Wheel` chama `leaseBinding.Expire()` quando o timeout estoura.
2. `tryExpire()` CAS (`Active` → `Expired`); se já estava liberada, não faz nada.
3. `finalizeRelease(false)` executa a mesma limpeza da devolução manual, mas contabiliza em `expiredCount`.
4. Contadores garantem rastreabilidade (`ReleasedCount` vs `ExpiredCount`).

### 5.7 Reset e Keep-Alive
- `Lease.ResetTimeout(timeout)`:
  1. Rejeita `timeout ≤ 0`.
  2. Garante estado `Active`.
  3. Obtém handle via `loadHandle()`; se `nil`, `ErrHandleUnavailable`.
  4. `handle.Reset(timeout)`:
     - Erro `wheel.ErrEntryExpired` → `ErrLeaseExpired`.
     - Outros erros são propagados.
  5. `binding.updateTimeout(timeout)` e `onLeaseTimeoutReset()` (contador).
- `Lease.KeepAlive()`:
  1. Mesma validação de estado/handle.
  2. `handle.KeepAlive()` chama `Reset(dealine)`.
  3. Converte `wheel.ErrEntryExpired` para `ErrLeaseExpired`.
  4. Incrementa `keepAliveCount`.

### 5.8 Encerramento (`Scheduler.Close`)
1. `CompareAndSwap` garante execução única (`Active` → `Closed`).
2. `collectActiveLeases()`:
   - Trava `registryMutex`, destacando toda a lista.
   - Retorna slice com leases ainda registradas (marcadas como não registradas).
3. Para cada lease coletada, `lease.binding.releaseOnSchedulerClose()`:
   - Pega o handle (se existir) e cancela.
   - CAS (`Pending`/`Active` → `Expired`).
   - `finalizeRelease(false)` devolve ao buffer.
4. `resourceBuffer.Close()` fecha o buffer subjacente.
- **Invariante**: após `Close`, não há leases ativas nem itens perdidos.

### 5.9 Métricas (`internal/scheduler/metrics.go`)
- `Scheduler.Metrics()` retorna snapshot contendo:
  - `IdleTimeout`.
  - `ActiveLeases`, `AcquiredCount`, `ReleasedCount`, `ExpiredCount`, `TimeoutResetCount`, `KeepAliveCount`.
  - `BufferMetrics` (capacidade, ocupação, disponibilidade) via `buffer.Metrics()`.
  - `WheelMetrics` (counters da wheel).
- Todos os campos são lidos via atomics e retornados por valor.
- Uso típico: CLI `metricscheck`, logging, observers futuros.

### 5.10 Pool de Leases (`internal/scheduler/lease_pool.go`)
- `acquire()` → pega de `sync.Pool` ou cria nova.
- `release(lease)`:
  - Chama `lease.resetForReuse()`:
    - Zera handle e mutex.
    - Remove ponteiros de registro.
    - `binding.resetStateForReuse()` limpa referencias e retorna estado `Pending`.
  - Devolve ao pool para uso futuro.
- Resultado: `Acquire` não gera alocação da `Lease`; a única alocação remanescente no caminho quente está na wheel (`scheduledHandle`), conforme descrito em `docs/architecture.md`.

---

## 6. Ferramentas e Verificações

### 6.1 CLI `cmd/metricscheck`
- Demonstração guiada da integração:
  1. Cria buffer estratégico (`capacity=4`).
  2. Instancia wheel (intervalo 50 ms, 64 slots) e chama `Start()`.
  3. Cria scheduler com `IdleTimeout=120 ms`.
  4. Pré-carrega valores (`Put`).
  5. Executa duas aquisições (`Acquire`).
  6. `Lease.Value` na primeira para demonstrar acesso.
  7. `KeepAlive` na primeira lease, `ResetTimeout` na segunda.
  8. `Release` manual da primeira.
  9. Aguarda 250 ms para a segunda expirar automaticamente.
  10. Imprime `Scheduler.Metrics()` exibindo contadores combinados.
  11. Tenta `Release` após expiração para ilustrar `ErrLeaseInactive`.
- **Intenção**: validar manualmente a consistência dos contadores e observar como as operações cooperam sem adicionar goroutines.

### 6.2 Benchmarks
- `internal/buffer/buffer_bench_test.go`:
  - Mede `Get`/`Release` nos modos estratégico e síncrono (baseline de latência e ausência de alocação).
- `internal/wheel/wheel_bench_test.go`:
  - `BenchmarkWheelScheduleCancel`: exercita agendamento + cancelamento.
  - `BenchmarkWheelHandleReset`: mede custo de resets consecutivos.
- `internal/scheduler/scheduler_bench_test.go`:
  - `BenchmarkSchedulerAcquireRelease`: ciclo completo de aquisição com `Wheel` real rodando.
- Resultados atuais (referência em `docs/progress.md`):
  - Buffer estratégico ≈ 52 ns/op, 0 alocações.
  - Wheel reset ≈ 52 ns/op, 0 alocações.
  - Wheel schedule+cancel ≈ 125 ns/op, 1 alocação (handle).
  - Scheduler acquire+release ≈ 350 ns/op, 1 alocação (handle).
- **Pendência**: pooling/embutimento do `scheduledHandle` para zerar a alocação restante (registrado em `docs/architecture.md`).

---

## 7. Invariantes Globais e Colaborações

1. **Limite de goroutines**: apenas a wheel roda em background; qualquer nova funcionalidade deve respeitar o teto de 5 goroutines.
2. **Entrega exclusiva**: cada item emprestado do buffer precisa ser devolvido exatamente uma vez (manual ou automática). A máquina de estados da lease e as CAS garantem isso.
3. **Pool consistente**: `wheelEntryPool` e `leasePool` devolvem objetos somente após todos os ponteiros sensíveis serem limpos (evita retenção de dados e alocações desnecessárias).
4. **Observabilidade leve**: contadores públicos (`WheelMetrics`, `Scheduler.Metrics`, `buffer.Metrics`) são snapshots por valor, sem alocação e sem locks adicionais.
5. **Preparação para extensões**: hooks (`WithExpireHook`, `WithRescheduleHook`, `WithReleaseErrorHandler`) e políticas (`SlotPolicy`) já estão expostos para encaixar observers, policies e telemetria sem refatorações.
6. **Documentação contínua**: toda a progressão técnica é registrada em `docs/architecture.md` (decisões, pendências) e `docs/progress.md` (linha do tempo funcional). Este arquivo fornece a interpretação detalhada exigida pelo projeto.

---

## 8. Diferenças em Relação à Referência (.reference)

Embora o diretório `.reference` traga a implementação original, o `crono` introduz:
- **Nomes descritivos e legibilidade estática**: nenhuma abreviação arbitrária.
- **Uso extensivo de atomics** em vez de mutexes generalizados.
- **Backoff reutilizável** (`ExponentialSleeper`) no lugar de sleeps ad-hoc.
- **Pooling real** de leases e entries (o legado alocava com frequência).
- **Integração documentada** entre buffer e wheel por meio do scheduler, mantendo limites rígidos de goroutines.
- **Infraestrutura de documentação e progresso** exigida pelas diretrizes.

---

## 9. Próximos Passos Registrados

- **Pooling do `scheduledHandle`**: item aberto em `docs/architecture.md`. Assim que abordado, atualizar benchmarks e métricas.
- **CLI parametrizável**: evolução do `metricscheck` (flags, JSON) para inspeção mais rica.
- **Observers/Policies**: hooks já existem; implementar conforme o roadmap da Trilha B5/C3.
- **Telemetria e inspeção**: utilizar métricas expostas para construir modos de auditoria sem alterar o caminho crítico.

---

Com estas seções, cada pacote, tipo e função do `crono` está descrito com a intenção original, seus efeitos e a forma como cooperam. Esse conhecimento é suficiente para continuar o desenvolvimento seguindo as diretrizes estabelecidas, identificar impactos de alterações futuras e justificar decisões de performance e sincronização.***
