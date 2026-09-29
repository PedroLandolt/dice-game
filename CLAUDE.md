# Regras do projeto

Contexto: exercício técnico para uma vaga de backend Go (iGaming). Estou a aprender Go e tenho de conseguir explicar cada linha deste código numa entrevista. O plano e as tarefas estão em `TASKS.md`.

## Código

- Go idiomático e simples, que alguém a começar em Go perceba. Preferir sempre a standard library.
- Dependências: só as listadas em `TASKS.md`. Para qualquer outra, pergunta primeiro e explica porquê.
- **Zero comentários no código.** Nenhum. O código tem de se explicar pelos nomes e pela estrutura.
- Funções curtas, com uma responsabilidade. Nada de abstrações "para o futuro", generics, reflection, builders, factories ou outros padrões enterprise.
- Interfaces só onde existem duas implementações reais (por exemplo, a real e a fake dos testes), definidas no package que as usa.
- Structs só com os campos necessários.
- Erros: devolver com contexto (`fmt.Errorf("...: %w", err)`); erros de domínio como `var ErrX = errors.New("...")`; nunca `panic` para controlo de fluxo.
- Dinheiro sempre `int64` em cêntimos. Nunca `float`.
- Aleatoriedade sempre com `crypto/rand`.
- `context.Context` como primeiro argumento em tudo o que faz I/O.
- `gofmt`, `go vet` e `golangci-lint` têm de passar.

## O código não pode parecer gerado

Este código vai ser lido por alguém que já viu projetos gerados por IA e que os reconhece. Tem de parecer escrito por uma pessoa com critério.

- Menos é melhor. Antes de acrescentar código, pergunta se é mesmo necessário para a tarefa.
- Nada de `Manager`, `Helper`, `Util`, `Handler` genérico, packages `utils`/`common`/`helpers`, nem wrappers que só chamam outra função.
- Nada de código morto: opções de config sem uso, funções não chamadas, `TODO`s, parâmetros ignorados.
- Nada de verificações defensivas para situações impossíveis.
- Logs só em pontos com significado (início e fim de um pedido, erros), não em cada passo.
- Um só estilo em todo o projeto: a mesma forma de tratar erros, de fazer o decode de JSON e de escrever respostas, sempre.
- Mensagens de erro em minúsculas, sem pontuação final (convenção do Go).
- Diffs pequenos. Se uma alteração passar das ~150 linhas, parte-a em passos.
- Documentação (README, docs) em tom técnico e direto: sem emojis, sem frases de marketing, sem secções vazias.
- No fim de cada fase, diz-me o que se pode apagar ou simplificar sem perder funcionalidade.

## Base de dados e fluxo

- O schema está todo em `db/init.sql`; não há ferramenta de migrações. O Postgres só corre este ficheiro quando o volume é criado, por isso depois de alterar o schema é preciso `docker compose down -v && docker compose up -d`. Diz-me sempre que isso for necessário.
- Não alteres a ordem do fluxo do Play (`pending` → debit na wallet → dado → `open`) nem do EndPlay sem discutir comigo primeiro.
- Sempre que mexeres no serviço de jogo, na wallet ou no storage das jogadas, explica no chat o que acontece se o processo cair entre cada passo.
- Os testes de integração saltam (`t.Skip`) quando `DATABASE_URL` não está definida.

## Nomes

- Nunca renomeies identificadores que eu escrevi.
- Se eu já escrevi as structs e as assinaturas, implementa só os corpos.
- Se precisares de tipos, structs, funções, métodos, constantes ou variáveis de package novos, propõe os nomes numa lista curta e **espera que eu escolha** antes de implementar. Variáveis locais triviais (`err`, `ctx`, `i`, `tx`) não precisam de aprovação.

## Forma de trabalhar

- Uma tarefa de `TASKS.md` de cada vez.
- Antes de escrever código, diz em poucas linhas o que vais fazer e que ficheiros vais tocar.
- No fim de cada tarefa, corre `go build ./...`, `go vet ./...` e os testes relevantes, e mostra o resultado.
- As explicações ficam no chat, nunca no código. Explica as partes menos óbvias sem que eu tenha de pedir.
- Não faças commits nem push. Sou eu que faço.
- Não marques tarefas como concluídas em `TASKS.md`. Sou eu que marco.
- Não cries ficheiros ou pastas fora da estrutura de `TASKS.md` sem perguntar.
- Se uma tarefa te parecer mal pensada ou houver uma forma mais simples, diz antes de implementar.
