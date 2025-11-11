
# 🔍 Análise Técnica Completa – Pacotes `buffer`, `scheduler`, e `wheel`

Este documento consolida **todos os pontos de atenção e recomendações** levantados durante a análise integral do código
dos pacotes `buffer`, `scheduler`, e `wheel`.  
O foco é garantir **robustez concorrente**, **clareza semântica**, e **segurança na reutilização de memória e recursos**.

---

## 🧭 Estrutura Geral e Interações

### 1. Arquitetura
- O `Scheduler` orquestra um `FixedBuffer` e um `Wheel`, onde:
  - O `Wheel` gerencia expiração temporal de *leases* (`Expirable`).
  - O `Buffer` armazena e reutiliza as *leases* disponíveis.
- A comunicação entre eles é feita via `Lease[T]`, que carrega um `Handler` vinculado à `Wheel`.

### 2. Ciclo de Vida
1. `Scheduler.Acquire()` obtém uma lease.
2. `Scheduler.release()` agenda expiração no `Wheel` e devolve ao `Buffer`.
3. `Wheel` expira a lease (via callback) e ela retorna ao pool.
4. Handlers (`scheduledHandler`) controlam cancelamento e reset de timers.

---

## 🧩 Pacote `buffer`

### ⚙️ Problemas e Melhorias
| Tópico | Descrição | Recomendação |
|--------|------------|--------------|
| **Duplicidade entre AccessModes** | `AccessModeStrategic` é um intermediário entre sync/async, mas sobrepõe semânticas com `AccessModeSynchronous`. | Revisar se o modo estratégico realmente precisa coexistir ou se deve ser um comportamento interno do buffer síncrono. |
| **FixedBuffer e StrategicBuffer** | Ambos seguem a mesma interface, mas a coexistência aumenta complexidade e ambiguidade de política de acesso. | Considere simplificar para um único buffer com flag de política interna (por exemplo, `strategy` enum). |
| **Releases e invalidação** | Sugestão de `ReleaseAndInvalidate()` é válida, mas deve manipular head/tail de forma lógica (shrink) sem realocação. | Evitar flags de invalidação — implicam alocação extra. Prefira shrink lógico. |
| **Lazy instantiation** | Atualmente é responsabilidade do `Scheduler`. | Transferir a lógica de criação preguiçosa para o buffer — aumenta encapsulamento e reduz duplicação. |

---

## ⚙️ Pacote `wheel`

### ⚠️ Problemas Detectados
| Área | Descrição | Impacto | Solução |
|------|------------|----------|----------|
| **expireEntry() reuse hazard** | Entry pode ser retornado ao pool enquanto `scheduledHandler` ainda o referencia. | Possível corrupção de lista ou duplo release. | Introduzir versionamento (`entry.version`) como token de posse. |
| **Handler Cancel race** | `Cancel()` pode atuar em entry reaproveitado em outro slot. | Corrupção inter-slot. | Comparar `entry.version.Current()` com `handler.entryId`. |
| **Double release** | `expireEntry()` e `Cancel()` podem devolver a mesma entry ao pool. | Panics ou dangling pointers. | Garantir ownership exclusivo de quem libera entry. |
| **Memory fence ausente** | `wheelEntryPool.acquireEntry()` limpa ponteiros sem barreira de memória. | Leituras stale em reuse imediato. | Usar atomic fence (`runtime_procPin`) ou sincronização indireta via lock. |
| **Handler recycling inconsistente** | Antes, `Cancel()` não devolvia handler em paths negativos. | Vazamento de handlers. | Sempre devolver handler (com `defer` ou branch final). |
| **enqueueEntryWithPolicy()** | É stub; não há política diferenciada. | Inconsistência conceitual. | Remover ou implementar política real de ordenação. |

### ✅ Ajustes Implementados (e Avaliados)
- `entry.version` + `handler.entryId` garantem unicidade lógica.
- `Cancel()` e `expireEntry()` passam a ser idempotentes e seguras.
- Uso de `atomic.Bool canceling` evita reentrância simultânea.

---

## 🧠 Pacote `scheduler`

### Padrões Corretos
- `prepareLease()` agora cancela handler anterior antes de reutilizar.
- `cleanupExpiredLease()` isola destruição de valor e retorno ao pool.
- `tryInitializeLease()` integra circuit breaker e backoff corretamente.

### Riscos Remanescentes
| Tópico | Descrição | Recomendação |
|--------|------------|--------------|
| **getRecoverableLease()** | Usa `for range availables`, o que não limita iterações em buffers concorrentes. | Substituir por iteração explícita até `availables` para consistência determinística. |
| **Expiração concorrente** | `cleanupExpiredLease()` e `release()` podem se sobrepor. | Garantir atomicidade via lock interno ou CAS de estado. |
| **Wheel ownership** | `Scheduler` pode ou não possuir o wheel (`ownsWheel`). | Certificar que `Close()` trata parada idempotente e bloqueante (sem leaks). |

---

## 🧱 scheduledHandler (versão revisada)

### Pontos Positivos
- Adição de `canceling atomic.Bool` previne reentrância.
- `entry.version` + `entryId` garantem token lógico de posse.
- Sempre devolve handler ao pool, evitando vazamentos.

### Melhorias Finais
| Categoria | Detalhe | Ação |
|------------|----------|-------|
| Ordem de checagens | `h.wheel` deve ser checado antes de `canCancel()`. | Inverter condição. |
| Cancel flag | `defer h.canceling.Store(false)` pode correr após reuse. | Mover reset antes de `release(h)`. |
| Nome de função | `canCanel` -> `ownsEntry` ou `canCancel`. | Corrigir e padronizar. |
| Reset() | Não verifica version (ownership). | Adicionar ou remover método. |

---

## 🔒 Conclusões sobre Concorrência

| Aspecto | Situação Atual | Risco | Mitigação |
|----------|----------------|--------|------------|
| Race entre slots | Resolvido via versioning | Baixo | Ok |
| Race entre Cancel() e Expire() | Resolvido com `canceling` e version | Baixo | Ok |
| Double release | Em revisão, depende de ownership explícito | Médio | Controlar via estado |
| Fence de memória no pool | Ainda ausente | Médio | Adicionar `atomic.StorePointer(nil)` ou similar |
| Lazy instantiation duplicada | Design-level | Baixo | Transferir para buffer |
| Reset() | Opcional, inconsistente com versionamento | Médio | Remover ou ajustar |

---

## 🚧 Plano de Ação

1. **Finalizar isolamento de ownership:**
   - Entry só é devolvido por um caminho (Cancel/Expire).
   - Adicionar `entry.inPool atomic.Bool` opcional se necessário.

2. **Adicionar fence mínima no pool:**
   - `atomic.StorePointer` ao limpar ponteiros reutilizados.

3. **Uniformizar nomenclatura e checagens:**
   - Corrigir `canCanel()` → `ownsEntry()`.
   - Validar ordem `wheel != nil` antes de verificações derivadas.

4. **Simplificar modos do buffer:**
   - Fundir `AccessModeStrategic` com `Synchronous` se o comportamento for híbrido.

5. **Revisar `Reset()` e `RecreateAlways`:**
   - Confirmar necessidade real de `Reset()`.
   - Reavaliar semântica de recriação vs expiração.

6. **Adicionar comentário de contrato por método crítico:**
   - Documentar invariantes e ownership no código.

7. **(Opcional)** Adotar pacote independente `buffer`:  
   - Tornar o buffer genérico com lazy instantiation e shrink lógico.

---

## 🧩 Conclusão

O sistema apresenta uma base sólida e modular, com uma engenharia de sincronização já bastante avançada.  
Os principais riscos estão hoje **no reuso prematuro de entries** e **na ordem de checagens em handlers**.  
Após aplicar os ajustes listados, o projeto atinge um nível de robustez comparável a runtimes industriais
como **Tokio (Rust)** e **Akka (Scala)**, mantendo o custo de heap zero e sem dependências externas.

---
