# Diretrizes para Agentes no Projeto `crono`

## Estilo e Código
- Escrever todo o código em inglês, usando nomes completos e descritivos; evitar abreviações arbitrárias.
- Receivers de métodos podem usar uma única letra (ex.: `func (b *Buffer)`) como exceção; demais nomes devem permanecer completos e claros.
- Manter legibilidade estática: funções curtas, responsabilidades claras e comentários apenas quando agregarem entendimento.
- Priorizar eficiência: minimizar alocações na heap, reutilizar estruturas pré-alocadas, empregar `sync/atomic` como abordagem principal e recorrer a mutex apenas como fallback controlado.
- Preservar e estender os padrões positivos do código atual (uso de `NoCopy`, backoff exponencial customizado, opções configuráveis).

## Organização e Documentação
- Atualizar `docs/progress.md` a cada estágio concluído, registrando a lógica das funcionalidades e explicando o comportamento das funções públicas e privadas implementadas.
- Registrar decisões técnicas relevantes, invariantes e justificativas de sincronização em `docs/architecture.md`.
- Garantir que a base de código esteja preparada para receber extensões futuras (observers, policies, telemetria, modos adicionais) através de interfaces e opções configuráveis, evitando refatorações grandes no futuro.

## Escopo do Desenvolvimento
- Tratar o repositório como composto por dois blocos principais: o buffer circular (`FixedBuffer`) e a time wheel (`Wheel`), além da futura integração `scheduler`. Implementar cada um de forma incremental sem misturar responsabilidades.
- Evitar introduzir testes formais ou benchmarks prematuramente; eles só devem ser criados após as funcionalidades principais estarem consolidadas.
- Não incluir funcionalidades de persistência de timers por enquanto; manter o design preparado, mas fora da implementação atual.

## Revisão Constante
- Reavaliar as premissas de performance e alocação a cada alteração significativa.
- Garantir que novas funcionalidades mantenham a compatibilidade com o planejamento e com as extensões futuras aprovadas (telemetria, sharding, inspeção, rate limiter, etc.), sem introduzir dependências externas desnecessárias.

## Commits
- Nunca assinar commits com seu nome ou indicar que foram gerados por agente; utilizar exclusivamente o usuário já configurado no repositório.
- Mensagens de commit devem ser curtas, objetivas e sem formalidade excessiva, descrevendo claramente o que foi alterado.
