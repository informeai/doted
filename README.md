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

Com zsh ou bash, o shell se comporta como uma sessão contínua: variáveis exportadas (`export`, `source .env`, `nvm use`), o diretório (inclusive um `cd` dentro de `cd pasta && make`), aliases e funções passam de um comando para o próximo. Ao abrir, o doted carrega os aliases e funções do seu `~/.zshrc` ou `~/.bashrc` em segundo plano, e eles contam como comandos válidos na coloração e nas sugestões. Cada comando continua sendo um processo próprio, por isso eles podem rodar lado a lado e ir para o background; o estado de um comando que terminou em background é descartado, para não desfazer o que veio depois. Variáveis sem `export` não passam de um comando para o outro. Em outros shells (sh, fish), cada comando começa do zero, como antes.

O histórico de comandos é salvo em `~/.local/share/doted/history` (ou `$XDG_DATA_HOME/doted/history`) e volta quando você abre o doted de novo, alimentando o ↑ e as sugestões. Como no bash, um comando que começa com espaço não é salvo, o que é útil para linhas com senhas ou tokens.

Enquanto você digita, a palavra do comando muda de cor: na cor de destaque se for um comando interno do doted, verde se existir no sistema (no `PATH`, um caminho executável como `./build.sh` ou um comando embutido do shell como `echo` e `export`) e vermelha se não existir. O `PATH` consultado é o mesmo que os comandos recebem.

Também como no fish, uma sugestão aparece apagada depois do cursor enquanto você digita, e Tab (ou → no fim da linha) aceita. Ela vem, nesta ordem: do comando mais recente do histórico que começa com o que você digitou; do nome de um comando (do doted, embutido do shell ou do `PATH`); ou de um arquivo ou pasta, no último argumento (`cd Pro` → `cd Projects/`). Entre nomes de comando, o mais curto vence; no empate, os do doted vêm primeiro. Ao aceitar com Tab, a bolinha do cursor some e um sublinhado elétrico corre sob o texto até o fim do comando completado, onde a bolinha reaparece com algumas faíscas (só com o cursor `dot` e as animações ligadas).

### Programas de tela cheia

`vim`, `less`, `htop`, `man`, `nano` e outros programas que ocupam a tela inteira funcionam: quando um deles entra na tela alternativa, a área de saída passa a mostrar a grade completa do terminal, com cores (na paleta do tema), cursor e movimentação livre, e volta para o histórico quando ele sai, sem deixar o conteúdo da tela no histórico. Enquanto isso, todas as teclas vão para o programa, inclusive Esc, Ctrl+B, PgUp/PgDn, F1–F12 e Alt, codificadas nos modos que ele pedir, e a roda do mouse vira setas. Com isso, `git log` e `man` voltam a usar o `less` como pager.

A emulação usa o [`charmbracelet/x/vt`](https://github.com/charmbracelet/x/tree/main/vt), que também responde às perguntas que esses programas fazem ao terminal (posição do cursor, tipo de terminal).

### Atalhos

| Tecla | Na entrada | Com comando rodando |
| --- | --- | --- |
| Enter | executa a linha | envia Enter ao programa |
| ↑ / ↓ | navega no histórico | envia as setas ao programa |
| Ctrl+R | busca no histórico | envia ao programa |
| Cmd+F (Ctrl+Shift+F no Linux e Windows) | busca na saída | busca na saída |
| Cmd+clique (Ctrl+clique no Linux e Windows) | abre o link ou arquivo clicado na saída | abre o link ou arquivo clicado na saída |
| Tab, → (no fim da linha) | aceita a sugestão mostrada depois do cursor | envia ao programa |
| Shift+←/→, Shift+Home/End | seleciona texto, marcado com pontinhos acima dos caracteres; digitar ou apagar substitui a seleção | — |
| Arrastar o mouse na saída (2 cliques: palavra, 3 cliques: linha) | seleciona o texto da saída | seleciona o texto da saída |
| Cmd+C / Cmd+X / Cmd+V (Ctrl+Shift+C/X/V no Linux e Windows) | copia (a seleção da saída, se houver), recorta e cola | copia a seleção da saída; cola no programa |
| Ctrl+C | descarta a linha | interrompe o programa (SIGINT) |
| Ctrl+B | — | manda o comando para o background |
| Ctrl+T | abre a lista de jobs | — |
| Ctrl+L | limpa a tela | envia ao programa |
| Ctrl+D | sai (com a linha vazia) | envia EOF |
| Ctrl+A / Ctrl+E | início / fim da linha | envia ao programa |
| Ctrl+U / Ctrl+W | apaga até o início / a palavra anterior, guardando o texto apagado | envia ao programa |
| Ctrl+Y | cola de volta o que o Ctrl+U/Ctrl+W apagou | envia ao programa |
| PgUp / PgDn, roda do mouse | rola o histórico | rola o histórico |

Para copiar a saída de um comando, arraste o mouse sobre ela: o trecho ganha um fundo na cor de destaque, e arrastar além do topo ou da base rola o histórico. A seleção fica presa ao texto, então não se desloca quando chega saída nova; Esc ou um clique a desfazem. Copiar, recortar e colar usam a área de transferência do sistema, então o texto vai e vem entre o doted e outros apps (no macOS pelo `pbcopy`/`pbpaste`, no Linux pelo `wl-copy`/`wl-paste`, `xclip` ou `xsel`, e no Windows pela API do sistema). Ao colar na linha de entrada, quebras de linha viram espaços, então um colar nunca executa um comando. O Ctrl+Y, como no bash, cola de volta o que o Ctrl+U/Ctrl+W apagou, sem mexer na área do sistema. Se o sistema não responder (no Linux sem nenhuma dessas ferramentas, por exemplo), o doted usa a própria área e avisa. Ao copiar ou colar, a barra de status confirma a ação por um instante.

No macOS, Cmd+←/→ vai para o início/fim da linha, Cmd+Backspace apaga até o início e Option+Backspace apaga a palavra anterior.

### Blocos por comando

Cada comando executado vira um bloco: a linha do comando e a saída abaixo dela. À direita da linha do comando aparece como ele foi:

- uma bolinha na cor de destaque, pulsando, e o tempo decorrido enquanto roda;
- uma bolinha verde e o tempo que levou (`1.2s`) quando termina bem;
- uma bolinha vermelha, o código de saída e o tempo (`exit 1 · 3.4s`) quando falha ou é morto;
- `job 2 in the background` quando foi para o background.

Com o mouse sobre a linha do comando, aparecem duas ações: **copy** copia a saída daquele comando (sem a linha do comando) e **rerun** executa o comando de novo. Clicar na barra do prompt, no começo da linha, recolhe a saída numa única linha `… 42 lines`; clicar de novo (ou nessa linha) expande.

### Busca no histórico e na saída

**Ctrl+R** abre a busca no histórico, que começa com o que já estava digitado. As letras digitadas não precisam estar juntas (`gco` encontra `git checkout main`) e aparecem destacadas nos resultados; a busca só diferencia maiúsculas se você digitar uma. ↑/↓ ou Ctrl+R de novo mudam o resultado, Enter coloca o comando no prompt (sem executar) e Esc volta.

**Cmd+F** (Ctrl+Shift+F no Linux e Windows) busca na saída da tela principal ou do job aberto. Todas as ocorrências ganham um fundo na cor de destaque, a atual um pouco mais forte, e a tela rola até ela. Enter vai para a anterior (mais antiga), Shift+Enter para a próxima, e a busca dá a volta nas pontas; se a ocorrência estiver num bloco recolhido, ele se expande. A busca acompanha a saída que continua chegando. Esc ou Cmd+F de novo fecham.

### Links e caminhos

Segurando **Cmd** (Ctrl no Linux e Windows), URLs e caminhos de arquivos que existem na saída ficam sublinhados sob o mouse, e um clique abre: URLs no navegador; arquivos no editor, na linha e coluna indicadas (`./cmd/main.go:42:7`, como nas mensagens de compiladores e testes). Caminhos relativos partem do diretório atual. O editor é o comando de `[links] editor`; sem ele, o doted usa o VS Code (`code -g`) se estiver instalado e, senão, o app padrão do sistema.

### Barra de status e notificações

Depois do diretório, a barra de status mostra o contexto do projeto. Primeiro vem o branch do git: o logo do Git, na cor do texto do tema (`foreground`), e o nome do branch em branco. Ao lado, o que mudou no repositório, cada tipo na sua cor: `~2` alterados (amarelo), `+1` adicionados (verde), `-1` removidos (vermelho), `?2` não rastreados, `!1` em conflito e `↑1`/`↓2` commits à frente/atrás do remoto. Depois, quanto tempo o último comando levou (`last 1.2s`). Se o caminho for longo, ele encurta para dar lugar ao contexto.

Quando o branch muda, o nome rola como o caminho no `cd` (só as letras que mudaram) enquanto o logo dá um quarto de volta. Num branch que ainda não tinha aparecido na sessão, como um recém-criado com `git switch -c`, o logo também solta faíscas. Ao entrar ou sair de um repositório, o logo, o nome e as mudanças aparecem ou somem com um fade. Com `[animation] enabled = false`, o branch só troca, e `particles = false` desliga as faíscas.

Apagar um branch ou uma tag também aparece na barra, depois do branch atual. O nome apagado surge na cor de erro, com um ícone de branch ou de etiqueta, um rastro elétrico, como o do autocompletar com Tab, corre pelo meio dele e as letras caem uma a uma, girando, até virarem faíscas vermelhas, enquanto o logo do Git sacode. Para saber o que foi apagado, o doted compara as refs do repositório (branches locais, tags e branches remotos) antes e depois de cada comando, então vale para `git branch -d`, `git tag -d`, `git push origin --delete`, `git fetch --prune`, aliases e outras ferramentas. Várias remoções de uma vez tocam uma depois da outra; a partir de quatro, viram um resumo como `5 branches`. O git roda em segundo plano, quando o diretório muda, depois de cada comando, quando a janela volta ao foco e a cada 15 segundos.

Quando um comando que levou pelo menos 10 segundos termina com o doted em segundo plano (outra janela em foco), o sistema mostra uma notificação com o resultado e o comando. No macOS ela vem pelo `osascript`, no Linux pelo `notify-send` e no Windows pelo PowerShell.

### Comandos internos

Digite `help` para ver a lista abaixo e os atalhos dentro do próprio doted (a barra de status lembra disso). Na lista, ↑/↓ seleciona um comando, Enter coloca ele no prompt para você completar e executar, e Esc fecha.

`cd`, `clear` e os demais comandos são os do próprio shell: o `cd` também muda o diretório do doted (e `cd -` volta para o anterior), e o `clear` limpa a tela.

- `jobs`: abre a lista de jobs
- `fg [n]`: abre o job `n` (ou o mais recente); também aceita `fg %n`
- `help`: lista os comandos e atalhos
- `exit` / `quit`: fecha o doted. Se houver jobs rodando, pede confirmação (repita o comando para matá-los e sair)

### Jobs em segundo plano

Comandos que prendem o terminal (servidores, watchers, builds longos) podem ir para o background e continuar rodando enquanto você usa o prompt:

- **Ctrl+B** com um comando rodando: ele vai para o background e o prompt fica livre.
- **`comando &`**: inicia o comando direto no background (`&&` continua funcionando normalmente).

Cada job guarda a própria saída desde o início, inclusive o que imprimiu enquanto estava em background. Quando um job termina, uma mensagem aparece na tela principal, e a barra de status mostra quantos jobs estão rodando.

**Lista de jobs** (Ctrl+T ou `jobs`): mostra cada job com status, tempo e a última linha de saída.

| Tecla | Ação |
| --- | --- |
| ↑ / ↓ | seleciona |
| Enter | abre o job |
| x | mata o job (se estiver rodando) ou remove da lista (se já terminou) |
| Esc / Ctrl+T | fecha a lista |

**Visão do job**: mostra a saída completa do job, e o teclado vai para ele enquanto estiver rodando. Ctrl+B volta para a tela principal sem parar o job; quando o job já terminou, Esc, Enter ou `q` também voltam. Ctrl+T abre a lista para trocar de job.

## Configuração

O tema padrão é azul, sobre fundo azul-marinho, e a fonte padrão é a monoespaçada do sistema.

O doted lê um arquivo TOML em `~/.config/doted/config.toml` (ou `$XDG_CONFIG_HOME/doted/config.toml`). Para gerar um arquivo com todas as opções comentadas:

```sh
doted -init-config
```

Só é preciso definir o que você quer mudar; o resto usa os padrões. As mudanças são aplicadas assim que o arquivo é salvo, sem reiniciar. Só o tamanho inicial da janela exige reiniciar, e ele vale apenas na primeira vez: depois, o doted reabre a janela do jeito que você a deixou. Se o arquivo tiver um erro, o doted mantém a configuração atual e mostra a mensagem no terminal. Para usar outro arquivo: `doted -config caminho/config.toml`.

Exemplo:

```toml
[font]
family = "JetBrains Mono"  # nome de uma fonte instalada ou caminho para .ttf/.otf/.ttc
size = 16
line_height = 1.4

[cursor]
style = "bar"              # dot, block, bar ou underline
animate = false            # a bolinha pula ao digitar e os outros estilos piscam

[prompt]
symbol = "$ "

[colors]
background = "#1e1e2e"
foreground = "#cdd6f4"
accent = "#f5c2e7"

[colors.normal]
red = "#f38ba8"

[shell]
program = "/bin/zsh"

[shell.env]
EDITOR = "nvim"
```

| Seção | Opções |
| --- | --- |
| `[font]` | `family`, `size`, `line_height` |
| `[window]` | `width`, `height` (tamanho da primeira janela; padrão 1280 × 800), `remember` (reabrir com o tamanho, a posição e o estado maximizado da última vez, guardados em `~/.local/state/doted/window.json`), `padding` |
| `[prompt]` | `style` (padrão `bar`: uma barra vertical na cor do texto que fica na cor de destaque e solta faíscas enquanto você digita; ou `symbol`), `symbol` |
| `[cursor]` | `style` (padrão `dot`: uma bolinha na cor do texto que fica na cor de destaque e pula enquanto você digita), `animate` |
| `[animation]` | `enabled` (liga ou desliga todas as animações), `fade_in_ms`, `particles` (as faíscas da barra) |
| `[scrollback]` | `lines` |
| `[history]` | `save` (guardar os comandos entre sessões), `lines` |
| `[clipboard]` | `system` (copiar e colar pela área de transferência do sistema; com `false`, fica tudo dentro do doted) |
| `[links]` | `editor` (comando que abre um arquivo clicado, com `{file}`, `{line}` e `{col}`; por exemplo `"zed {file}:{line}:{col}"`) |
| `[notify]` | `enabled`, `after_seconds` (quanto um comando precisa durar para notificar; padrão 10) |
| `[status]` | `context` (branch do git com os arquivos alterados e duração do último comando na barra de status) |
| `[shell]` | `program`, `[shell.env]` |
| `[colors]` | `background`, `foreground`, `muted`, `accent`, `error`, `border`, `cursor` |
| `[colors.normal]` / `[colors.bright]` | `black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, `white` |

Os padrões e a descrição de cada opção estão em [`internal/config/default.toml`](internal/config/default.toml).

Sobre fontes:

- Com um nome, as variantes negrito e itálico da mesma família são escolhidas automaticamente, incluindo fontes variáveis.
- Com um caminho de arquivo, a mesma face é usada para todas as variantes.
- A primeira busca por nome indexa as fontes do sistema e pode levar alguns segundos; o índice fica em cache.
- Por padrão (`family = ""`) é usada a fonte monoespaçada do sistema: SF Mono no macOS (ou Menlo) e a `monospace` do fontconfig no Linux.
- `family = "Go Mono"` usa a fonte embutida no binário, que também é o último recurso quando nenhuma outra é encontrada.
- Se uma fonte pedida pelo nome não for encontrada, o doted usa a Go Mono e avisa. Fontes que não são monoespaçadas também geram aviso.

## Pacotes e CI

O workflow `.github/workflows/ci.yml` roda `go vet` e os testes no Linux, macOS e Windows. Depois disso, gera um pacote para cada sistema:

| Sistema | Pacote | Conteúdo |
| --- | --- | --- |
| macOS | `doted-<versão>-macos-universal.dmg` | `doted.app` universal (Apple Silicon + Intel), com atalho para Aplicativos |
| Linux | `doted_<versão>_amd64.deb` | `/usr/bin/doted` e atalho no menu de aplicativos |
| Windows | `doted-<versão>-windows-x64.msi` | instala em `Program Files\doted` com atalho no Menu Iniciar |

Cada pacote é instalado e verificado no próprio CI, e fica disponível como artefato da execução. Ao publicar uma tag `v1.2.3`, os três pacotes são anexados a uma release no GitHub. Em builds sem tag, a versão é `0.0.<número da execução>`.

Os scripts também funcionam localmente:

```sh
packaging/macos/build-dmg.sh 0.1.0 dist            # no macOS
packaging/linux/build-deb.sh 0.1.0 dist amd64      # precisa de dpkg-deb
pwsh packaging/windows/build-msi.ps1 -Version 0.1.0 # precisa do WiX 5 (dotnet tool install --global wix --version 5.0.2)
```

O ícone do app é desenhado por `tools/icongen`, que é a única fonte da geometria. O `go generate ./assets/icon` gera a partir dele o SVG, os PNGs do Linux, o `.ico`, o `.icns` do macOS e o `rsrc_windows_amd64.syso`, que embute o ícone no `doted.exe`. Os arquivos gerados ficam no repositório. Um teste (`go test ./tools/icongen`) compara esses arquivos com uma renderização nova e falha se estiverem desatualizados. A comparação tem uma pequena tolerância, porque a renderização varia alguns níveis de cor entre processadores arm64 e amd64.

Observações:

- O `.app` usa assinatura ad hoc, sem Apple Developer ID nem notarização. Na primeira abertura, o macOS bloqueia o app: clique com o botão direito em `doted.app` e escolha **Abrir**.
- Quando aberto pelo Finder ou pelo menu de aplicativos, o doted começa na pasta pessoal e carrega o ambiente do shell de login (PATH do `.zprofile`/`.profile`), como os outros terminais.
- No Windows o doted instala e abre, mas ainda não executa comandos, porque o suporte a PTY (ConPTY) ainda não foi implementado.
- O instalador usa o WiX Toolset 5, a última versão apenas sob a licença MS-RL. As versões 6 e posteriores exigem a Open Source Maintenance Fee.

## Estrutura

```
main.go                  flags, carga da configuração e janela do Ebitengine
.github/workflows/ci.yml testes nos três sistemas, pacotes e release em tags
packaging/               scripts do .dmg (macos/), .deb (linux/) e .msi (windows/)
assets/icon/             ícone gerado em todos os formatos (SVG, PNG, .ico, .icns)
tools/icongen/           desenha o ícone e gera esses arquivos (go generate)
internal/
  app/                   o "jogo" do Ebitengine
    app.go               Update: teclado, rolagem, comandos internos
    draw.go              Draw: layout, texto com estilos, cursor e animações
    keys.go              tradução de teclas para bytes de terminal
    settings.go          configuração + fontes, e recarga ao salvar o arquivo
    theme.go             cores e paleta ANSI/256 cores
    commands.go          envio da linha e comandos internos (jobs, fg, help...)
    jobs.go              background, lista de jobs e visão do job
    blocks.go            blocos por comando: resultado, copy/rerun e recolher
    histsearch.go        busca no histórico (Ctrl+R)
    find.go              busca na saída (Cmd+F)
    links.go             URLs e caminhos clicáveis
    statuscontext.go     leitura do git status em segundo plano
    gitbadge.go          branch e mudanças do git na barra de status
    gitlogo.go           logo do Git desenhado a partir do SVG oficial
    gitanim.go           animações do branch: rolagem, giro do logo, faíscas e fade
    gitdelete.go         animação ao apagar branches e tags
  config/                arquivo TOML: padrões (default.toml), validação e watcher
  fonts/                 resolução da fonte por nome ou arquivo, com variantes
  jobs/                  jobs em execução ou finalizados, cada um com sua saída
  notify/                notificações do sistema (osascript, notify-send, PowerShell)
  shell/                 sessão (shell, ambiente, diretório) e processos em PTY
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

Há também um teste de renderização que abre uma janela e passa pelo fluxo completo (comando anexado, background, lista de jobs, visão do job), executando o `Draw` de verdade:

```sh
go test -tags smoke ./internal/app/
```

## Limitações atuais

- O cursor só se move dentro da linha atual; sequências que movem o cursor para outras linhas são ignoradas.
- Jobs em background não são pausados (não há Ctrl+Z/SIGTSTP): eles continuam rodando.
- No Linux, a área de transferência do sistema precisa do `wl-clipboard` (Wayland) ou do `xclip`/`xsel` (X11); o `.deb` recomenda um deles.
- Caracteres largos (CJK, emoji) desalinham a grade, e a fonte Go Mono tem cobertura limitada de símbolos.
