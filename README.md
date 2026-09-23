# doted

Emulador de terminal escrito em Go, renderizado com [Ebitengine](https://ebitengine.org/) para permitir animações na interface.

O layout segue o estilo de um terminal de chat: você digita na caixa de entrada na parte de baixo e a saída dos comandos aparece acima dela, com a linha mais recente sempre encostada na entrada.

```
 ...saída anterior
 > ls
 README.md  go.mod  internal  main.go
 ───────────────────────────────────────
 > git status█
 ───────────────────────────────────────
 ~/Projects/doted
```

## Requisitos

- Go 1.26+
- macOS ou Linux (os comandos rodam em um PTY via `creack/pty`, que ainda não suporta Windows)
- No Linux, as dependências de sistema do Ebitengine (X11/OpenGL). Veja o [guia de instalação](https://ebitengine.org/en/documents/install.html).

## Rodando

```sh
go run .
```

Ou gere o binário:

```sh
go build -o doted .
./doted
```

## Uso

Cada comando roda em `$SHELL -c` dentro de um pseudo-terminal, no diretório atual do doted. Enquanto um comando está rodando, o teclado vai direto para ele, então prompts de senha, perguntas `y/n` e REPLs como `python3` funcionam.

### Atalhos

| Tecla | Na entrada | Com comando rodando |
| --- | --- | --- |
| Enter | executa a linha | envia Enter ao programa |
| ↑ / ↓ | navega no histórico | envia as setas ao programa |
| Ctrl+C | descarta a linha | interrompe o programa (SIGINT) |
| Ctrl+L | limpa a tela | envia ao programa |
| Ctrl+D | sai (com a linha vazia) | envia EOF |
| Ctrl+A / Ctrl+E | início / fim da linha | envia ao programa |
| Ctrl+U / Ctrl+W | apaga até o início / a palavra anterior | envia ao programa |
| PgUp / PgDn, roda do mouse | rola o histórico | rola o histórico |

No macOS, Cmd+←/→ vai para o início/fim da linha, Cmd+Backspace apaga até o início e Option+Backspace apaga a palavra anterior.

### Comandos internos

- `cd [dir]`: muda o diretório usado pelos próximos comandos (aceita `~`)
- `clear`: limpa a tela
- `exit` / `quit`: fecha o doted

## Estrutura

```
main.go                  janela e loop do Ebitengine
internal/
  app/                   o "jogo" do Ebitengine
    app.go               Update: teclado, rolagem, comandos internos
    draw.go              Draw: layout, texto com estilos, cursor e animações
    keys.go              tradução de teclas para bytes de terminal
    theme.go             cores, paleta ANSI/256 cores e fonte
  shell/                 execução de comandos em PTY (entrada, saída, resize, kill)
  terminal/              modelo sem dependência de UI
    parser.go            interpretação da saída do programa (texto, SGR, CR/BS, erase)
    scrollback.go        histórico de linhas exibido acima da entrada
    editor.go            linha de entrada com cursor e histórico
```

O pacote `terminal` não depende do Ebitengine, o que facilita testá-lo isoladamente.

## Testes

```sh
go test -race ./...
```

## Limitações atuais

- Programas de tela cheia (`vim`, `top`, `less`, `htop`) ainda não são emulados. Quando um deles entra na tela alternativa, a barra de status avisa. Por isso `PAGER` e `GIT_PAGER` são definidos como `cat`.
- O cursor só se move dentro da linha atual; sequências que movem o cursor para outras linhas são ignoradas.
- Um comando por vez, sem controle de jobs.
- Sem colar da área de transferência (o Ebitengine não expõe clipboard).
- Caracteres largos (CJK, emoji) desalinham a grade, e a fonte Go Mono tem cobertura limitada de símbolos.
