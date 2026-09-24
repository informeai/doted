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

### Barras de progresso e caracteres largos

Barras de progresso de várias linhas, como as do `docker pull`, `npm`, `pnpm`, `cargo` e `pip`, são redesenhadas no lugar: o doted segue o cursor quando o programa sobe linhas (`ESC[A`, `ESC[F`), vai a uma posição da tela (`ESC[H`), apaga abaixo dele (`ESC[J`) ou salva e restaura sua posição (`ESC 7`/`ESC 8`), sempre dentro das linhas do próprio comando. No fim fica só o resultado final, sem uma cópia de cada atualização, e reescrever uma linha não repete a animação de entrada.

Caracteres largos, como chinês, japonês, coreano e emoji, ocupam duas colunas, como num terminal, e não desalinham o que vem depois. O que a fonte configurada não tem é buscado em fontes do sistema: símbolos, emoji coloridos e CJK (no macOS, Apple Symbols, Apple Color Emoji e Hiragino; no Linux, o que o fontconfig indicar; no Windows, Segoe UI Symbol, Segoe UI Emoji e Microsoft YaHei). Essas fontes são lidas sob demanda, sem carregar o arquivo inteiro na memória. Selecionar, copiar, buscar e clicar em links contam cada caractere largo uma vez.

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
- `#2 in the background` quando foi para o background.

Com o mouse sobre a linha do comando, aparecem duas ações: **copy** copia a saída daquele comando (sem a linha do comando) e **rerun** executa o comando de novo. Clicar na barra do prompt, no começo da linha, recolhe a saída numa única linha `… 42 lines`; clicar de novo (ou nessa linha) expande.

### Busca no histórico e na saída

**Ctrl+R** abre a busca no histórico, que começa com o que já estava digitado. As letras digitadas não precisam estar juntas (`gco` encontra `git checkout main`) e aparecem destacadas nos resultados; a busca só diferencia maiúsculas se você digitar uma. ↑/↓ ou Ctrl+R de novo mudam o resultado, Enter coloca o comando no prompt (sem executar) e Esc volta.

**Cmd+F** (Ctrl+Shift+F no Linux e Windows) busca na saída da tela principal ou do job aberto. Todas as ocorrências ganham um fundo na cor de destaque, a atual um pouco mais forte, e a tela rola até ela. Enter vai para a anterior (mais antiga), Shift+Enter para a próxima, e a busca dá a volta nas pontas; se a ocorrência estiver num bloco recolhido, ele se expande. A busca acompanha a saída que continua chegando. Esc ou Cmd+F de novo fecham.

### Links e caminhos

Segurando **Cmd** (Ctrl no Linux e Windows), URLs e caminhos de arquivos que existem na saída ficam sublinhados sob o mouse, e um clique abre: URLs no navegador; arquivos no editor, na linha e coluna indicadas (`./cmd/main.go:42:7`, como nas mensagens de compiladores e testes). Caminhos relativos partem do diretório atual. O editor é o comando de `[links] editor`; sem ele, o doted usa o VS Code (`code -g`) se estiver instalado e, senão, o app padrão do sistema.

### Barra de status e notificações

Depois do diretório, a barra de status mostra o contexto do projeto. Primeiro vem o branch do git: o logo do Git, na cor do texto do tema (`foreground`), e o nome do branch em branco. Ao lado, o que mudou no repositório, cada tipo na sua cor: `~2` alterados (amarelo), `+1` adicionados (verde), `-1` removidos (vermelho), `?2` não rastreados, `!1` em conflito, `↑1` commits para enviar (na cor de destaque) e `↓2` commits para puxar (ciano). Depois, quanto tempo o último comando levou (`last 1.2s`). Se o caminho for longo, ele encurta para dar lugar ao contexto.

Quando o branch muda, o nome rola como o caminho no `cd` (só as letras que mudaram) enquanto o logo dá um quarto de volta. Num branch que ainda não tinha aparecido na sessão, como um recém-criado com `git switch -c`, o logo também solta faíscas. Ao entrar ou sair de um repositório, o logo, o nome e as mudanças aparecem ou somem com um fade. Com `[animation] enabled = false`, o branch só troca, e `particles = false` desliga as faíscas.

Apagar um branch ou uma tag também aparece na barra, no lugar do branch atual. O nome do branch atual rola até o nome apagado, que fica na cor de erro, e o logo do Git fica vermelho. Um rastro elétrico, como o do autocompletar com Tab, corre pelo meio do nome enquanto o logo sacode, e as letras caem uma a uma, girando, até virarem faíscas vermelhas. No fim, o nome do branch atual sobe de volta ao lugar e o logo volta à cor normal. Para saber o que foi apagado, o doted compara as refs do repositório (branches locais, tags e branches remotos) antes e depois de cada comando, então vale para `git branch -d`, `git tag -d`, `git push origin --delete`, `git fetch --prune`, aliases e outras ferramentas. Várias remoções de uma vez tocam uma depois da outra; a partir de quatro, viram um resumo como `5 branches`.

Renomear um branch (`git branch -m`) usa a animação de checkout, não a de remoção. Renomear o branch atual é como trocar para o novo nome: o nome rola e o logo gira. Renomear outro branch acontece no lugar do atual: o nome atual rola até o nome antigo, que rola para o novo enquanto o logo gira, e depois o branch atual volta. O doted reconhece uma renomeação quando uma ref some e outra do mesmo tipo aparece apontando para o mesmo commit. Tags e branches remotos renomeados (como num `git remote rename`) não animam.

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

Cada job guarda a própria saída desde o início, inclusive o que imprimiu enquanto estava em background, e a barra de status mostra quantos jobs estão rodando. Com a faixa de jobs ligada (o padrão), a tela principal fica limpa: o cartão mostra o número do job (`#2`, o mesmo do `fg 2`), e a linha do comando mostra `#2 in the background` e, no fim, o resultado. Com a faixa desligada, mensagens como `[2] running in background` e `[2] done` avisam na tela principal.

**Faixa de jobs ao vivo**: cada job em background aparece como um cartão no topo da janela, com o número do job e um nome curto (`dev` para `npm run dev`, `watch` para `make watch`), há quanto tempo roda e as últimas linhas da saída, atualizadas ao vivo. Não há layout para gerenciar: o cartão desce do topo quando o job vai para o background e sai sozinho depois que ele termina. No fim do job, o cartão fica inteiro por um instante (com a borda se acendendo na cor do resultado) e depois se recolhe numa pílula com o resultado, como `✓ #3 build · 1.2s` ou `✗ #2 test · exit 1`. A pílula fica até completar 6 segundos do fim (20 quando o job falha) e então sobe e sai pelo topo. Os cartões deslizam até o seu lugar em vez de pular: um novo cresce no espaço enquanto os outros abrem caminho, e os vizinhos ocupam aos poucos o lugar de um que sai. Clicar na pílula também abre o job. A bolinha mostra o estado: cor de destaque, pulsando, enquanto roda; verde quando termina bem; vermelho quando falha.

Os cartões têm cara de cartão: ficam sobre uma sombra suave, com um brilho discreto na borda de cima, e sobem um pouco sob o mouse. A borda conta o que está acontecendo: enquanto chega saída nova, um feixe de luz na cor de destaque percorre o contorno, e some quando o job fica quieto; quando o job imprime erros, o feixe fica vermelho e a borda pulsa; quando o job termina, a borda se acende numa volta completa, verde ou vermelha, e depois volta ao normal. Com `[animation] enabled = false`, a borda fica parada.

Os cartões leem a saída enquanto ela chega:

- uma linha que parece erro (`error`, `FAIL`, `panic`, `fatal`, `Traceback`) deixa o cartão vermelho, com um brilho que pisca uma vez, e aparece em vermelho no cartão; uma linha de sucesso depois (`ok`, `PASS`, `ready`, `compiled`), como num watcher que volta a passar, desfaz isso;
- a primeira URL local que o job imprime (`http://localhost:5173/`, típica de servidores de desenvolvimento) vira um link no cartão.

Clicar num cartão, ou Ctrl+número, abre a visão completa do job. O número do atalho é o número do job (o `#3` do cartão, o mesmo do `fg 3`), não a posição na tela, então funciona mesmo com o cartão fora de vista. Números de mais de um dígito são digitados em sequência com a tecla segurada: Ctrl+1 e depois Ctrl+2 abre o `#12`. O atalho vai na hora quando nenhum outro job começa com o que foi digitado (com até 9 jobs, é sempre imediato); senão, espera o próximo dígito, até você soltar a tecla ou por 0,6 segundo, mostrando na barra de status `open #1…` e já selecionando o cartão do número digitado.

Para não poluir a tela, a faixa mostra no máximo 4 cartões, sempre os dos primeiros jobs. Os jobs seguintes ficam agrupados num cartão à direita, desenhado como uma pilha, com `+3` e uma bolinha por job na cor do estado (rodando, com erro, concluído). Ao selecionar o grupo (com Alt+→ depois do quarto cartão ou com um clique), ele se abre: os jobs agrupados ocupam a faixa, e o grupo vai para a esquerda como cabeçalho (`‹ +3`). A seleção segue por eles com ←/→ (Enter no grupo leva ao primeiro). Esc, um clique no grupo aberto ou voltar com Alt+← para antes do grupo o fecham, e a faixa volta a mostrar os 4 cartões e o grupo. Alt+número e Ctrl+número de um job agrupado também abrem o grupo nele. O limite muda em `[jobs] strip_cards`.

O grupo acompanha o que acontece com os jobs dele. Quando um cartão fixo sai, o próximo job do grupo sai da pilha e desliza até a vaga, com um brilho na cor de destaque. Quando um job agrupado termina, o grupo pisca na cor do resultado e a barra de status avisa (`#6 migrate finished in 1.0s` ou `#6 test failed (exit 1) · alt+6 to see it`). Um job agrupado que terminou bem sai depois de 2,5 segundos, já que o cartão dele não está à vista; um que falhou continua 20 segundos, para dar tempo de ir até ele. Sempre que a quantidade muda, o número rola (`+3` → `+2`) e a pilha dá um pequeno pulo.

Cada cartão mostra as 3 últimas linhas da saída (`[jobs] strip_lines`). O cartão selecionado cresce para baixo e mostra as 10 últimas (o mesmo vale para o que está recebendo o teclado), sobre a saída e sem empurrar a faixa nem os outros cartões, que continuam do mesmo tamanho; ele volta ao normal quando a seleção sai. Para ver tudo, basta abrir o job (Enter ou clique).

Quando os cartões visíveis não cabem, a faixa primeiro recolhe os jobs quietos (sem saída há mais de 10 segundos) em cartões de uma linha, como `● #2 worker · 12m`; os que estão ativos, com erro, selecionados ou recebendo o teclado continuam inteiros. Depois, os cartões se estreitam até um mínimo legível. Só se nem assim couber (numa janela muito estreita) a faixa vira um carrossel: setas nas pontas mostram quantos cartões estão fora de vista de cada lado (`‹ 2`, `3 ›`), e clicar nelas ou usar a roda do mouse sobre a faixa rola, com animação.

Pelo teclado, **Alt+← / Alt+→** selecionam um cartão (com borda de destaque) e rolam a faixa até ele. Com um cartão selecionado, ←/→ continuam andando entre eles, **Enter** abre o job, **Alt+S** envia para ele, **Alt+R** reinicia, **Alt+.** para e **Esc** tira a seleção. Qualquer outra tecla também tira a seleção e segue para o shell normalmente. Também dá para controlar o job sem abri-lo. Com o mouse sobre o cartão, aparecem três ações:

- **restart** para o job e, quando ele termina (para um servidor liberar a porta), roda o mesmo comando de novo no mesmo lugar da faixa;
- **stop** envia Ctrl+C; se o job não parar, o botão vira **kill** por alguns segundos e um segundo clique o mata;
- **send** (ou Alt+número, com os dígitos do mesmo jeito) aponta a linha de entrada para o job: o prompt mostra `→ dev ›`, o cartão ganha uma borda na cor de destaque e mais linhas, e o que você digita vai para ele, sem sair da tela principal. Se o job lê uma linha por vez (um `read`, uma pergunta `y/n`, o `rs` do nodemon), você edita a linha no doted e o Enter a envia; se ele lê tecla a tecla (o `r` e o `q` do Vite, o `a` e o `p` do Jest e do Vitest, um REPL), cada tecla vai na hora. O doted descobre sozinho qual é o caso pelo modo do terminal do job. Esc devolve a linha ao shell, com o que você tinha digitado antes; se o job terminar, ela volta sozinha.

Jobs com programas de tela cheia (vim, `less`, `git log`, htop) não escrevem no histórico, mas numa tela à parte. Enquanto um deles está em tela cheia, o cartão mostra as linhas dessa tela em volta do cursor, com o selo `full screen`. Ao enviar para ele (**send** ou Alt+número), o cartão se abre numa janela flutuante sobre a saída, com a tela inteira do programa em tamanho normal, sem sair da tela principal: a faixa de jobs e a barra de status continuam visíveis. O terminal do job ganha o tamanho da janela enquanto ela está aberta, e o programa se redesenha para ela. Todas as teclas vão para o programa, inclusive o Esc; **Ctrl+B** fecha a janela, que volta para o cartão, como ao sair da visão de um job. Para desligar a faixa ou mudar quantas linhas cada cartão mostra, use `[jobs] strip` e `strip_lines`.

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
| `[jobs]` | `strip` (a faixa de jobs ao vivo no topo da janela), `strip_lines` (quantas linhas cada cartão mostra; padrão 3), `strip_cards` (quantos cartões aparecem antes de agrupar os demais; padrão 4) |
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
    strip.go             faixa de jobs ao vivo no topo da janela
    cardborder.go        sombra e bordas animadas dos cartões
    cardlife.go          entrada, pílula de resultado, saída, mini cartões e carrossel
    stripnav.go          seleção de cartões e atalhos pelo número do job
    jobfloat.go          janela flutuante para enviar a jobs em tela cheia
    jobcontrol.go        restart, stop e enviar entrada aos jobs pelos cartões
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

- O cursor de um comando só volta até a primeira linha que ele escreveu; programas que redesenham a tela inteira sem usar a tela alternativa podem sobrar linhas acima.
- Jobs em background não são pausados (não há Ctrl+Z/SIGTSTP): eles continuam rodando.
- No Linux, a área de transferência do sistema precisa do `wl-clipboard` (Wayland) ou do `xclip`/`xsel` (X11); o `.deb` recomenda um deles.
- Na linha de entrada, caracteres largos (CJK, emoji) ainda contam como uma coluna, então o cursor fica deslocado depois deles; na saída eles ocupam as duas colunas certas. Emoji compostos (famílias, bandeiras, tons de pele) aparecem como seus caracteres separados.
