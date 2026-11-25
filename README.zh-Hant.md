# agentport

[English](README.md) | [Português (Brasil)](README.pt-BR.md) | [Español](README.es.md) | **繁體中文**

agentport 讓在 SSH 伺服器上執行的程式碼代理程式，在你授權的前提下使用你電腦上的連接埠。代理程式可以看到伺服器上正在監聽的程序，並要求在你的電腦上開啟某個連接埠，走的是你既有的 SSH 連線。開啟哪些連接埠、何時關閉，都由你決定。

> **開發中。** 基礎部分已可使用：Companion、SSH 反向通道、MCP 伺服器、在各代理程式中的自動註冊、安裝程式、可驗證的發行版本、需經你核准的連接埠工具、面板（分組、批次關閉、偵測到的連接埠分頁，以及連到伺服器的網頁終端機），以及 Companion 重新啟動後轉發的復原。Windows 主機已在 CI 中編譯，但尚未在真正的 Windows 上執行。連線時自動啟動，以及 `v0.1.0` 的端到端驗證仍未完成。下方的狀態表會說明哪些已完成、哪些是規劃中。

**目錄**

- [這是什麼](#這是什麼)
- [狀態](#狀態)
- [運作方式](#運作方式)
- [需求](#需求)
- [安裝](#安裝)
  - [安裝腳本](#安裝腳本)
- [設定伺服器](#設定伺服器)
- [自動啟動](#自動啟動)
- [日常使用](#日常使用)
- [面板](#面板)
- [終端機](#終端機)
- [MCP 工具](#mcp-工具)
- [多個代理程式工作階段](#多個代理程式工作階段)
- [主機上被占用的連接埠](#主機上被占用的連接埠)
- [獨立模式](#獨立模式)
- [語言](#語言)
- [安全性](#安全性)
- [更新](#更新)
- [解除安裝](#解除安裝)
- [疑難排解](#疑難排解)
- [開發](#開發)
- [致謝](#致謝)
- [授權](#授權)

## 這是什麼

agentport 由三個部分組成：

- **Companion**：在你電腦上於背景執行的程序。它在 `127.0.0.1` 上維持本機橋接器（bridge），為每個已配對的伺服器保持一條 SSH 反向通道，連線中斷時以 1 到 30 秒的退避時間重新連線。
- **MCP 伺服器**（`agentport mcp`）：由代理程式在伺服器上透過 `stdio` 啟動。agentport 會把它註冊到伺服器上找到的代理程式中。
- **面板**：位於你電腦上的網頁，讓你核准、檢視、分組與關閉代理程式要求的連接埠，並可開啟連到伺服器的終端機。

你可以設定伺服器、以 `ssh` 連線，在代理程式中使用 `agentport_status` 工具（它會回報通道狀態，以及是否有更新的發行版本），並請代理程式在你的電腦上開啟連接埠，且需經你核准。

> **截圖預留位置。** 面板的截圖會加在這裡。

agentport 是 [AgentClip](https://github.com/wendellrocha/agentclip) 的分支，後者的方向相反（從你的電腦到伺服器）。請見[致謝](#致謝)。

## 狀態

| 階段 | 內容 | 狀態 |
| --- | --- | --- |
| F1 | 專案基礎：AgentClip 的分支，已改名、移除剪貼簿功能。CLI、Companion、`setup`、附 `agentport_status` 的 MCP 伺服器、發行版本 | ✓ 完成 |
| F1 | 專案基礎：AgentClip 的分支，已改名、移除剪貼簿功能。CLI、Companion、`setup`、附 `agentport_status` 的 MCP 伺服器、發行版本 | ✓ 完成 |
| F3 | 轉發引擎（`internal/forward`），以及查出誰佔用主機連接埠的功能（`internal/hostport`） | ✓ 完成；由連接埠工具與面板使用 |
| F1b | 由發行流程產生的 Homebrew 公式、能辨識 Homebrew 的 `agentport upgrade`，以及這四份 README | ✓ 完成；在 tap 存在之前，發布到 tap 的功能維持關閉 |
| F2 | 伺服器端連接埠掃描器（`internal/portscan`），支援 Linux 與 macOS，並可過濾系統常駐程式；`agentport ports` 指令 | ✓ 完成 |
| F4 | MCP 連接埠工具（`ports_list`、`ports_forward`、`ports_unforward`、`forwards_list`）、經核准的開啟、橋接器端點、工作階段，以及 Companion 重新啟動後的復原 | ✓ 完成 |
| F5 | 面板：核准、分組與批次關閉、偵測到的連接埠分頁、通知、連接埠被占用時的選擇 | ✓ 完成 |
| F5（其餘） | 連線時自動啟動（`LocalCommand`） | 規劃中 |
| F5b | 面板中連到伺服器的網頁終端機（Linux 與 macOS） | ✓ 完成 |
| F6 | Windows 主機：不使用 ControlMaster 的轉發 | 已在 CI 編譯；尚未在真正的 Windows 上執行 |
| 測試 | `test/e2e` 的 Docker 測試環境：真實的 sshd、經由 ControlMaster 的轉發、連接埠衝突與 Linux 上的掃描器 | ✓ 完成 |
| F7 | 在真實伺服器上做端到端驗證，發行 `v0.1.0` | 規劃中 |

標示為*規劃中*的項目不在目前版本內，請暫時不要依賴它們。

## 運作方式

```text
HOST（你的電腦）                              SERVER（經由 SSH 連到）

 Companion                                    代理程式（Claude Code、Codex 等）
   127.0.0.1 上的本機橋接器                       |  stdio，由代理程式啟動
        ^                                       v
        |                                      agentport mcp
        +---- SSH 反向通道（ssh -R）-------------+
              每台伺服器一條連線
```

1. `agentport setup` 會在伺服器上安裝 agentport、把它的 MCP 伺服器註冊到伺服器上找到的代理程式中、在你的電腦上儲存一份**設定檔**（伺服器、配對權杖與遠端連接埠），然後啟動 Companion。
2. Companion 以反向通道開啟一條 `ssh` 連到伺服器。透過這條通道，伺服器可以連到橋接器，因此 `agentport mcp` 能在伺服器的 loopback 介面上的某個連接埠找到它。
3. 代理程式在伺服器上透過 `stdio` 啟動 `agentport mcp`。MCP 伺服器使用工作階段權杖，經由通道與橋接器溝通。
4. 連接埠工具會要求 Companion 開啟某個連接埠，並由你核准。Companion 透過同一條 SSH 連線開啟（macOS 與 Linux 上使用 ControlMaster 的 `ssh -O forward -L`；Windows 上每個轉發使用一個 `ssh` 程序），因此連接埠只會抵達你電腦的 `127.0.0.1`。你的網路上不會有任何東西在監聽。

每台伺服器都有自己的設定檔與自己的 Companion，因此兩台伺服器絕不會共用權杖或通道。

## 需求

**你的電腦（主機）**

- macOS（Apple silicon 或 Intel）、Linux（amd64 或 arm64）或 Windows（amd64）。Windows 安裝程式、Companion 與轉發會在 CI 中編譯，但尚未在真正的 Windows 上執行。伺服器終端機在 Windows 上無法使用。
- `PATH` 中要有 OpenSSH 用戶端（`ssh`）。Companion 用它建立通道，因此會照常讀取你的 `~/.ssh/config`、金鑰與 ssh-agent。
- 只有在從原始碼建置時才需要 Go 1.27 或更新版本。

**伺服器**

- Linux 或 macOS，amd64 或 arm64，並且有你能登入的 SSH 伺服器。第一次執行 `setup` 時可能會要求輸入一次密碼；之後會改用金鑰。
- `AllowTcpForwarding yes`：伺服器 SSH 設定（`sshd_config`）中必須允許。反向通道使用 `ssh -R`，當轉發設為 `no` 時伺服器會拒絕。OpenSSH 的預設值是 `yes`；可在伺服器上以 `sshd -T` 檢查實際生效的值。
- POSIX shell，因為 `setup` 會透過 SSH 執行一組簡短且固定的指令。
- 至少安裝一個支援的程式碼代理程式（見下表）。agentport 不會安裝代理程式。

**支援的代理程式（harness）**

agentport 整合的是 *harness*，也就是執行代理程式並呼叫其工具的程式，而不是它所使用的模型。`setup`、`pair` 與 `connect` 會偵測已安裝的 harness，只設定這些。

| Harness | 狀態 | 整合方式 |
| --- | --- | --- |
| Claude Code | 支援 | 安裝後自動偵測並設定 |
| Codex | 支援 | 安裝後自動偵測並設定 |
| Gemini CLI | 支援 | 安裝後自動偵測並設定 |
| AGY / Antigravity CLI | 支援 | agentport 會建立或更新全域 MCP 項目 |
| OpenCode | 支援 | agentport 會建立或更新全域 MCP 項目 |
| Pi Coding Agent | 支援 | agentport 會安裝全域擴充功能 |
| 其他 MCP 用戶端 | 手動 | 將它指向 `agentport mcp`，並自行設定設定檔變數 |

DeepSeek 與 MiMo 是模型，不是 harness：請透過 Pi 或 OpenCode 等支援的 harness 使用它們。

## 安裝

### Homebrew

> **規劃中。** 發行流程已經會產生公式，但 tap `rafaelb13/homebrew-tap` 尚未發布，因此下列指令目前還無法使用。在 tap 存在之前，請使用安裝腳本或從原始碼建置。

tap 發布後：

```bash
brew install rafaelb13/tap/agentport
agentport version
brew upgrade agentport
brew uninstall agentport
```

安裝由 Homebrew 負責。若你是用 Homebrew 安裝的，`agentport upgrade` 不會替換執行檔：它會印出 `agentport was installed with Homebrew. Run: brew upgrade agentport` 並以狀態碼 0 結束。自動啟動不需要 `brew services`：Companion 透過 `agentport companion autostart` 啟動（見[自動啟動](#自動啟動)）。

### 安裝腳本

在 macOS 或 Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/RafaelB13/agentport/main/scripts/install.sh | sh
```

指定版本或目錄：

```bash
curl -fsSL https://raw.githubusercontent.com/RafaelB13/agentport/main/scripts/install.sh | sh -s -- --version vX.Y.Z --install-dir "$HOME/.local/bin"
```

在 Windows 的 PowerShell 中：

```powershell
irm https://raw.githubusercontent.com/RafaelB13/agentport/main/scripts/install.ps1 | iex
```

若要指定版本，請先下載腳本，再以 `-Version` 執行：

```powershell
.\install.ps1 -Version vX.Y.Z
```

腳本會偵測你的平台、查詢最新發行版本、驗證它（見下文），並與已安裝的版本比較。只有在發行版本較新時才會下載，因此重複執行是安全的。選項如下：

| 選項 | Shell | PowerShell | 環境變數 |
| --- | --- | --- | --- |
| 版本 | `--version vX.Y.Z` | `-Version vX.Y.Z` | `AGENTPORT_VERSION` |
| 安裝目錄 | `--install-dir 路徑` | `-InstallDir 路徑` | `AGENTPORT_INSTALL_DIR` |
| 儲存庫 | | | `AGENTPORT_REPOSITORY` |
| 略過建置證明檢查 | | | `AGENTPORT_SKIP_ATTESTATION=1` |
| 語言 | | | `AGENTPORT_LANG` |

Windows 的預設安裝目錄是 `%LOCALAPPDATA%\Programs\agentport\bin`。發行版本也列在 [GitHub Releases 頁面](https://github.com/RafaelB13/agentport/releases)。

另有兩個選用設定，僅適用於 shell 腳本：

- `GITHUB_TOKEN`：對儲存庫具有讀取權限的權杖。儲存庫為私有時請設定它。腳本會改經由 GitHub API 下載發行版本的資產，且權杖只會送往 `api.github.com`。
- `AGENTPORT_DRY_RUN=1`：列出封存檔、校驗和、證明與安裝目錄的位置後即結束，不下載也不安裝任何東西。

腳本會將每個下載檔與發行版本的 SHA-256 校驗和比對，不符或缺少項目時即停止。若要在執行前先閱讀腳本，請先下載：

```bash
curl -fsSLO https://raw.githubusercontent.com/RafaelB13/agentport/main/scripts/install.sh
less install.sh
sh install.sh
```

### 從原始碼建置

```bash
git clone https://github.com/RafaelB13/agentport.git
cd agentport
go build -o agentport ./cmd/agentport
```

把產生的 `agentport` 放到 `PATH` 中。從原始碼建置的二進位檔沒有發行版本號，因此 `agentport version` 不會顯示發行號，更新檢查也不適用於它。

### 驗證發行版本

每個發行版本都會發布 `checksums.txt`，它以 [cosign](https://github.com/sigstore/cosign) 簽章（無金鑰，綁定發行工作流程的身分），另外還有 GitHub 的建置證明。每個封存檔都有 SHA-256 項目，證明涵蓋封存檔、SBOM 與檢查碼。若要手動驗證，請下載該發行版本的 `checksums.txt`、`checksums.txt.bundle` 與 `attestation.jsonl`：

```bash
VERSION=vX.Y.Z
# 身分需要發行版本的確切標籤，因此不接受從其他分支執行的結果。
IDENTITY="https://github.com/RafaelB13/agentport/.github/workflows/release.yml@refs/tags/${VERSION}"

cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

# Linux
sha256sum --check --ignore-missing checksums.txt
# macOS（沒有 sha256sum，也沒有 --ignore-missing）
ARCH=$(uname -m | sed s/x86_64/amd64/)
grep " agentport_${VERSION}_darwin_${ARCH}.tar.gz$" checksums.txt | shasum -a 256 --check

gh attestation verify "agentport_${VERSION}_linux_amd64.tar.gz" \
  --repo RafaelB13/agentport \
  --cert-identity "$IDENTITY"
```

不加 `--bundle` 的 `gh attestation verify` 會向 GitHub 查詢，並需要先執行 `gh auth login`。若要在不登入的情況下驗證，請加上 `--bundle attestation.jsonl`。

**自動檢查。** 從 `v0.7.1-rc.1` 開始，安裝程式與 `agentport upgrade` 除了檢查 SHA-256，還會確認該檔案是由本儲存庫的發行工作流程，針對要安裝的標籤所產生的。若此檢查失敗，安裝會被拒絕。安裝腳本在安裝了 `gh` 時使用 `gh attestation verify`，否則改查 GitHub 的建置證明 API。`agentport upgrade` 使用 API。`v0.7.1-rc.1` 之前的發行版本沒有建置證明；它們只以 SHA-256 檢查安裝，安裝程式也會說明這一點。

若建置證明 API 無法使用或超過速率限制，安裝會被拒絕，而不是在沒有檢查的情況下繼續。若只想以 SHA-256 檢查安裝，請設定 `AGENTPORT_SKIP_ATTESTATION=1`。`agentport setup` 與 `agentport upgrade` 也會把同樣的設定套用到它們送往伺服器的二進位檔。

## 設定伺服器

`agentport setup` 用一道指令就能設定好伺服器：

```bash
agentport setup devbox --profile m2
ssh devbox
codex
```

這裡的 `devbox` 是你 `~/.ssh/config` 中的 SSH 別名。接著請代理程式檢查連線：「檢查 agentport 的狀態。」它會呼叫 `agentport_status`。

`setup` 的語法：

```text
agentport setup <ssh目的地> [--profile 名稱] [--agent all|codex|claude|gemini|agy|opencode|pi] [--version vX.Y.Z] [--remote-port 39123] [--skip-agent] [--skip-install] [--no-start]
```

`setup` 依序做以下事情：

1. **檢查 SSH。** 它先嘗試不需互動的金鑰登入。若成功，你原有的 SSH 設定會保持不變。
2. **必要時建立專用金鑰。** 若伺服器只接受密碼，`setup` 會建立只供 agentport 使用的 Ed25519 金鑰，要求你輸入一次密碼，把金鑰加入伺服器的 `authorized_keys`，之後的通道都改用這把金鑰。金鑰存放在你使用者的設定目錄（`agentport/keys/`），權限為私人專用。
3. **在伺服器上安裝二進位檔**，位置是 `~/.local/bin/agentport`。這台電腦會下載並驗證適用於伺服器平台的二進位檔（它會讀取伺服器的 `uname`），再透過 SSH 傳送過去。伺服器上若已有相同或更新的版本，就不會被更動。若要略過這一步，請使用 `--skip-install`。
4. **在伺服器找到的代理程式中註冊 agentport**，除非使用 `--skip-agent`。`--agent` 會把範圍限制在單一 harness。
5. **在你的電腦上儲存設定檔**，內含目的地、配對權杖與遠端連接埠。設定檔位於 `<使用者設定目錄>/agentport/profiles/`（macOS 上為 `~/Library/Application Support/agentport/profiles/`）。檔案中含有配對權杖，請妥善保管。
6. **啟動 Companion**，除非使用 `--no-start`。

對同一個設定檔再次執行 `setup`，會產生新的配對權杖，並停止為它執行中的 Companion。

### 選項

| 選項 | 意義 |
| --- | --- |
| `--profile 名稱` | 配對的本機名稱。未指定時，會從目的地推導。 |
| `--agent 名稱` | 把設定限制在單一 harness：`codex`、`claude`、`gemini`、`agy`、`opencode`、`pi`，或預設的 `all`。 |
| `--version vX.Y.Z` | 要安裝到伺服器上的發行版本。 |
| `--remote-port N` | 伺服器上供反向通道使用的 loopback 連接埠。預設 `39123`。 |
| `--skip-agent` | 不在伺服器上註冊 MCP 整合。 |
| `--skip-install` | 不在伺服器上安裝或更新二進位檔。 |
| `--no-start` | 完成設定但不啟動 Companion。 |

### 伺服器目的地

目的地就是你會交給 `ssh` 的內容：`~/.ssh/config` 中的別名、主機名稱，或 `使用者@主機`。連接埠、金鑰與跳板主機都放在 SSH 設定中，因此需要它們的伺服器請使用別名：

```text
# ~/.ssh/config
Host devbox
  HostName 10.0.0.12
  User rafael
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump jumphost
```

**規劃中：** `setup` 將以選項接收連接埠、金鑰與跳板主機（`-p`、`-i`、`-J`），並支援帶連接埠的 IPv6 位址（`使用者@[2001:db8::12]:2222`）。在那之前，請使用別名。

## 自動啟動

Companion 在你的電腦上執行，因此必須處於運作狀態，通道才會存在。有兩種方式讓它自行啟動。

**登入時（現在可用）。** `agentport companion autostart enable m2` 會安裝系統本身的登入機制：macOS 上是 `~/Library/LaunchAgents` 中的 LaunchAgent，Linux 上是 `~/.config/systemd/user` 中的 systemd 使用者單元，Windows 上則是登入時執行的工作排程器工作。該服務會執行 `agentport companion serve m2`，失敗時會自動重新啟動。若已有一個 Companion 在執行，它不會再開一個。

```bash
agentport companion autostart enable m2
agentport companion autostart status m2
agentport companion autostart disable m2
```

`disable` 會停止由該服務啟動的 Companion。你自己啟動的 Companion 會繼續執行。

**連到伺服器時（規劃中，F5）。** 經你同意後，`setup` 會在你的 `~/.ssh/config` 中加入一個區塊，讓連到伺服器時，若 Companion 未執行就會自動啟動：

```text
# agentport: profile m2
Match host 10.0.0.12 user rafael
  PermitLocalCommand yes
  LocalCommand agentport companion ensure m2
```

該區塊會以 `# agentport: profile` 註解劃定範圍，因此刪除這些行即可移除它。在那之前，沒有需要移除的區塊。

**若只有伺服器重新啟動**，而 Companion 仍在你的電腦上執行，Companion 會自行重新建立通道，退避時間為 1 到 30 秒。會再次使用相同的設定檔與遠端連接埠。

## 日常使用

照常連到伺服器並啟動代理程式：

```bash
ssh devbox
codex
```

隨時都可以請代理程式檢查連線：

> 檢查 agentport 的狀態。

代理程式會呼叫 `agentport_status`，告訴你通往 Companion 的通道是否已連線，以及是否有更新的穩定發行版本。若要在你的電腦上查看相同資訊：

```bash
agentport companion status m2
```

你可以這樣要求：

> 幫我對應這個專案的連接埠，並在 localhost 上開啟應用程式。

代理程式會列出伺服器上正在監聽的連接埠，然後逐一說明理由，要求開啟它需要的連接埠。每個要求都會以一筆批次要求的形式出現在面板中，即使包含很多連接埠，你也可以一次全部核准或拒絕，或逐一處理。核准的連接埠會開在你電腦的 `127.0.0.1` 上，代理程式會告訴你本機網址。若本機連接埠已被占用，agentport 會改用下一個可用的連接埠並說明原因。

## 面板

面板是位於你電腦上的網頁，服務於 `127.0.0.1`，並以權杖保護。它會顯示：

- **代理程式的要求**，位於頂端：連接埠、理由、指令、核准或拒絕，以及核准的有效期（本工作階段、一小時或永久）。批次要求會變成一張附有勾選框與「全部核准」的卡片。
- **已開啟的連接埠**，格式為 `:遠端 -> localhost:本機`，附名稱、指令、即時流量、在新分頁開啟、釘選、詳細資料、編輯與關閉。關閉時會先要求確認，並顯示哪些代理程式工作階段會收到通知。
- **分組與批次關閉**：依要求、依代理程式工作階段，或以平面清單分組；可選取多個連接埠；一次確認即可關閉某個群組或所選項目。
- **切換伺服器時**若仍有已開啟的連接埠，會詢問要關閉、保留或取消，並可選擇記住這個選擇。
- **已關閉的連接埠**，附上時間與關閉者，以及**重新開啟**：會沿用相同設定，若原本的本機連接埠已被占用，則建議一個可用的連接埠。
- **伺服器上偵測到的連接埠**，以及手動開啟的選單（快捷鍵 `/`）。
- **依需求顯示的程序詳細資料**：PID、使用者、CPU、記憶體、目錄、父程序、容器、指令列、連接埠健康狀態，以及代理程式提出的理由。
- **活動紀錄**、通知、快捷鍵，以及**頂端列**，包含目前的伺服器、切換伺服器、網頁終端機、核准政策、語言、主題（系統、淺色、深色）、說明與結束工作階段。
- **網頁終端機**：可從面板開啟、連到伺服器的終端機。它使用設定檔的 SSH 連線，並以面板的權杖保持在 `127.0.0.1`（見[終端機](#終端機)）。
- **連到伺服器**：規劃中，會提供在你的電腦上找到的連線建議（見[獨立模式](#獨立模式)）。

面板可用 `agentport companion open m2` 開啟，並需要其中的權杖。其中也會顯示通道狀態（`Connected` 或 `Disconnected`，英文原文）、最後一次錯誤，以及有更新的穩定發行版本時的提示；`agentport companion stop m2` 會停止 Companion。面板不傳輸檔案。

## 終端機

面板有一個 **Terminal** 分頁，可以在已連線的伺服器上開啟 shell，不必離開瀏覽器就能執行快速指令。它沿用通道的 SSH 連線（不需要再次登入），只提供給 `127.0.0.1` 上的面板，並且只接受來自面板本身頁面的連線。最多可同時開啟四個終端機。關閉終端機或停止 Companion，都會結束伺服器上的 SSH 工作階段。終端機內容絕不寫入記錄檔。Windows 版本目前不提供此終端機。

## MCP 工具

代理程式透過伺服器上的 MCP 伺服器看到以下工具。

| 工具 | 輸入 | 回傳 |
| --- | --- | --- |
| `agentport_status` | 無 | **現在可用。** 通道狀態、agentport 版本，以及是否有更新的穩定發行版本（若有，會建議執行 `agentport upgrade`）。 |
| `ports_list` | `include_system?`、`only_user?` | 伺服器上正在監聽的 TCP 連接埠：連接埠、bind 位址、協定、PID、程序、簡短指令列，以及是否已有轉發。 |
| `ports_forward` | `reason`、`group?`、`ports[]`，每項為 `{remote_port, label?, path?}` | 以一筆批次要求向你請求核准。對每個連接埠回傳狀態（`active`、`pending_approval`、`denied`、`skipped`）、實際使用的本機連接埠，以及含 `path` 的網址。最多等待 2 分鐘；若你尚未回覆，會回傳 `pending_approval`，代理程式之後可以再呼叫 `forwards_list` 查詢。 |
| `ports_unforward` | `ports[]` 或 `all` | 關閉本工作階段的轉發。絕不會關閉屬於其他工作階段的連接埠。 |
| `forwards_list` | 無 | 目前作用中的轉發，含來源（代理程式、自動或手動）、啟用時間與位元組數。其他工作階段的轉發會標示出來，本工作階段無法關閉它們。 |

代理程式永遠無法選擇你的本機連接埠，也沒有任何工具能讓它停止你電腦上的程序。停止程序是你的決定，只能在面板中進行。

若工具被呼叫時 Companion 沒有在執行，該工具會回答 Companion 處於離線狀態，並指示代理程式執行 `agentport companion start <設定檔>`，代理程式會把這件事轉達給你。

## 多個代理程式工作階段

每台伺服器由一個 Companion 服務。該伺服器上的每個代理程式工作階段（例如兩個位於不同專案中的 Claude Code，或 Claude Code 與 Codex）都會執行自己的 `agentport mcp`，而且全都與同一個 Companion 溝通。每個工作階段都以它的代理程式、PID、專案資料夾與開始時間來識別。

- 只有 Companion 會選擇本機連接埠，而且一次處理一個要求，因此兩個工作階段絕不會拿到相同的本機連接埠。
- 若兩個工作階段要求同一個遠端連接埠，該連接埠只會開啟一次。兩者會拿到相同的網址，並都成為它的擁有者。當沒有任何擁有者仍需要它，或你將它關閉時，連接埠才會關閉。
- `ports_unforward` 只會把呼叫它的工作階段從其連接埠的擁有者清單中移除。仍被其他工作階段使用的連接埠會保持開啟。
- 工作階段結束時，只屬於它的連接埠會被標記為「工作階段已結束」，並在 20 分鐘後關閉，除非你已釘選它們。若代理程式在這段時間內回到同一個資料夾，就會取回它們。
- 若 Companion 重新啟動或通道中斷，原本開啟的轉發會在連線恢復時被重新接回，因此工作階段會保留它們的連接埠，不必再次要求。

## 主機上被占用的連接埠

當你要求的本機連接埠已在你的電腦上被占用時，轉發不會失敗。Companion 會改用其上方的下一個可用連接埠，而訊息會指出佔用該連接埠的程序，例如 `local port 3000 is busy: node (pid 1234)`。若無法識別擁有者，訊息會顯示 `another program`。面板提供下方的選項；Companion 在開啟連接埠之前會再次檢查（擁有者由 `internal/hostport` 識別）。

| 佔用者 | 對話框提供的選項 |
| --- | --- |
| 你自己的程序（node、vite、python） | 改用其他連接埠，或停止該程序：先 SIGTERM，5 秒後 SIGKILL（Windows 上為 `taskkill`）。 |
| 會自行重新啟動的服務（`brew services`、LaunchAgent、systemd user） | 改用其他連接埠，或以正確的指令停止服務（例如 `brew services stop php`），執行前會先顯示。 |
| Docker 容器 | 改用其他連接埠，或執行 `docker stop <容器>`。絕不會停止 Docker daemon。 |
| 其他使用者或系統 | 改用其他連接埠。停止選項會停用並附上說明。面板絕不會要求系統管理員密碼。 |

停止之後，Companion 會再次檢查連接埠。若仍被占用，就改用替代連接埠，並把這件事記錄在活動紀錄中。

## 獨立模式

**規劃中。** `agentport open` 會在沒有任何設定檔的情況下開啟面板，並提供一個連到伺服器的畫面。它會建議在你電腦上找到的連線，而且不會把任何資料傳送到別處：

- `~/.ssh/config` 及其 `Include` 檔案中的主機（含萬用字元的項目，例如 `Host *.corp`，會被略過），其使用者、連接埠、金鑰與跳板主機由 `ssh -G <別名>` 解析。
- VS Code 的 Remote-SSH 設定。
- 目前正在開啟的 `ssh` 工作階段，標示為「目前已連線」。
- 在 Windows 上，已儲存的 PuTTY 工作階段。

只有在你為該次搜尋勾選選項時，才會使用終端機歷史紀錄（`~/.zsh_history`、`~/.bash_history`）。`known_hosts` 不作為來源，因為其中的名稱通常經過雜湊。每個建議都有**測試**（`ssh -o BatchMode=yes`，用來確認金鑰能否通過驗證、是否要求密碼，以及 agentport 是否已安裝）與**設定**（與 `setup` 相同的流程）。你也可以直接輸入 `使用者@ip`。

## 語言

agentport 支援四種語言：English（預設）、Português（Brasil）、Español，以及繁體中文（台灣用語）。

命令列、`help`、安裝程式與 Companion 會依下列順序，從環境中選擇語言：

1. `AGENTPORT_LANG`
2. `LC_ALL`
3. `LC_MESSAGES`
4. `LANG`

第一個有設定的變數決定語言，與 C 函式庫的做法相同。`pt`、`pt_BR`、`pt_PT` 等值使用 Português（Brasil）；`es` 與 `es_*` 使用 Español；`zh_TW`、`zh_HK`、`zh_MO` 與 `zh-Hant` 使用繁體中文。`C`、`POSIX` 以及其他任何值，包括 `zh_CN` 與 `zh-Hans`，都使用英文。因此 `LANG=pt_BR.UTF-8 agentport help` 會印出葡萄牙文，而 `AGENTPORT_LANG=en` 會強制使用英文。

Companion 網頁依序使用：網址中的 `?lang=pt-BR`、瀏覽器偏好的語言、Companion 程序依上述方式偵測到的語言，最後才是英文。

命令列與 Companion 都有 Português（Brasil）、Español 與繁體中文的翻譯目錄。安裝程式則提供英文與 Português（Brasil）的訊息。技術性的錯誤訊息刻意保留英文，方便搜尋。指令名稱、路徑與程序名稱不會翻譯。
## 安全性

- 橋接器只監聽 `127.0.0.1`。伺服器只能透過 Companion 開啟的 SSH 通道連到它。
- 伺服器是你信任範圍的一部分。伺服器上的代理程式只有在使用 Companion 為它註冊的工作階段權杖時，才能連到橋接器。
- 配對權杖儲存在你的設定檔中。不要把設定檔或權杖放進會進入專案儲存庫的檔案中。
- Companion 網頁位於 `127.0.0.1`，並需要一個包含在其網址中的權杖。不要分享這個網址：它是通往你 Companion 的入口。
- 發行版本由單一工作流程建置，附有 SHA-256 檢查碼、cosign 簽章、建置證明，並會以 `govulncheck` 掃描二進位檔。請見[驗證發行版本](#驗證發行版本)。

適用於連接埠工具與面板：

- 轉發只會在你電腦的 `127.0.0.1` 上 bind。第 1 版沒有 `0.0.0.0` 的選項。
- 轉發的目標永遠是伺服器上的 `127.0.0.1:<連接埠>`，因此不能利用通道去連到伺服器網路中的其他主機。
- 代理程式要求的每個連接埠都需要你核准，除非你為它選擇「永遠」。
- 封鎖的連接埠（22、2375、2376），以及每個設定檔同時轉發數量的上限。
- 停止你電腦上的程序只能在面板中進行，並需確認，而且絕不會以系統管理員權限執行。它不是 MCP 工具。

## 更新

`agentport upgrade` 會下載適用於這台電腦的最新穩定發行版本，檢查其 SHA-256 與建置證明，然後替換這個執行檔。接著會依下列順序更新所有已儲存的伺服器。

1. 這台電腦下載並驗證二進位檔（這是最慢的步驟；輸出會說明）。
2. 逐一更新各伺服器。某台伺服器失敗不會阻止其他伺服器，摘要會列出每個失敗的設定檔。
3. 直到此時才會停止執行中的 Companion、替換本機執行檔，並重新啟動那些 Companion。本機替換放在最後，是因為它是唯一難以復原的步驟。

若這台電腦已經是最新的發行版本，這裡不會下載或替換任何東西；只會更新落後的伺服器。

**使用 Homebrew 時**，二進位檔屬於 Homebrew 的 keg，因此 `agentport upgrade` 不會替換它。它會印出 `agentport was installed with Homebrew. Run: brew upgrade agentport`，然後把所有已儲存的伺服器更新到這個二進位檔的版本，與任何安裝方式相同。若要讓這台電腦升級到較新的發行版本，請執行 `brew upgrade agentport`；下一次 `agentport upgrade` 會把伺服器更新到該版本。

只有在你願意僅以 SHA-256 檢查時，才設定 `AGENTPORT_SKIP_ATTESTATION=1`。

Companion 會在啟動時檢查一次更新的穩定發行版本，之後最多每 24 小時檢查一次，並在各設定檔之間共用此檢查結果。較新的發行版本會顯示在 Companion 網頁與 `agentport_status` 中。

## 解除安裝

1. **為每個設定檔停止自動啟動與 Companion**：

   ```bash
   agentport companion autostart disable m2
   agentport companion stop m2
   ```

2. **在伺服器上，從每個 harness 移除 agentport 的項目**：

   ```bash
   agentport uninstall m2 --agent claude
   ```

   對你設定過的每個 harness 重複執行。這只會移除 agentport 的項目或擴充功能，不會移除代理程式本身。

3. **若沒有其他設定檔使用，請從伺服器移除二進位檔**：

   ```bash
   ssh devbox 'rm -f ~/.local/bin/agentport'
   ```

4. **移除本機檔案**：`<使用者設定目錄>/agentport/profiles/m2.json` 中的設定檔、`<使用者設定目錄>/agentport/keys/` 中的專用金鑰（若曾建立），以及 `<使用者快取目錄>/agentport/logs/` 中的日誌。

5. **移除你電腦上的二進位檔**：
   - Homebrew：`brew uninstall agentport`。
   - 安裝腳本或原始碼：刪除其目錄中的 `agentport` 檔案（macOS 與 Linux 預設為 `~/.local/bin`，Windows 預設為 `%LOCALAPPDATA%\Programs\agentport\bin`）。

**規劃中：** `uninstall` 也會移除 `setup` 加入的 `~/.ssh/config` 區塊，並會提供刪除設定檔的指令。

## 疑難排解

**Companion 沒有在執行。** 用 `agentport companion start m2` 啟動它，或啟用[自動啟動](#自動啟動)。`agentport companion status m2` 會說明它是否在執行，以及通道為何中斷。

**通道一直中斷。** 用 `agentport logs m2` 查看日誌路徑，或用 `agentport logs m2 --export ./agentport-m2.log` 匯出。若要在重現問題時記錄通道事件：

```bash
agentport companion stop m2
agentport companion start m2 --verbose
```

詳細模式會記錄通道事件與錯誤，絕不會記錄權杖或內容。

**`setup` 一直要求輸入密碼。** 伺服器目前不接受你這個使用者的公開金鑰登入，或其 `~/.ssh/authorized_keys` 無法寫入。請先確認一般的 `ssh` 金鑰登入可以運作；`setup` 只有在密碼能成功使用一次時，才能加入專用金鑰。

**「could not verify the build attestation」。** 無法連到建置證明 API，或該發行版本沒有建置證明。請檢查網路，或安裝 `gh` 後重試。只有在你願意僅以 SHA-256 檢查時，才設定 `AGENTPORT_SKIP_ATTESTATION=1`。

**「SSH destination must not start with a dash」。** 目的地看起來像一個選項。請提供主機、別名或 `使用者@主機`。

**`upgrade` 說 agentport 是用 Homebrew 安裝的。** 依照訊息執行 `brew upgrade agentport`。

**`agentport doctor` 說橋接器無法使用。** 啟動某個設定檔的 Companion。`doctor` 只檢查本機橋接器；`companion status` 還會回報通道。

## 開發

建置、測試、發行流程與訊息目錄都在 [DEVELOPMENT.md](DEVELOPMENT.md)。貢獻規範在 [CONTRIBUTING.md](CONTRIBUTING.md)，安全政策在 [SECURITY.md](SECURITY.md)。指令參考請見 [COMMANDS.md](COMMANDS.md)。

**端到端測試環境。** `test/e2e` 會在 Docker 中啟動一台一次性的 `sshd`，並針對它檢查經由真實 ControlMaster 的轉發、連接埠衝突，以及 Linux 內的連接埠掃描器。它需要 Docker 正在執行，且不會動到你的 `~/.ssh`：

```bash
test/e2e/run.sh
go test -tags e2e -v -count=1 ./test/e2e/
```

一般的 `go test ./...` 不會執行這個測試環境。

**規劃中（F7）：** 整合完成後，同一個測試環境會端到端地執行 `setup` 與連接埠工具。

## 致謝

agentport 起源於 [AgentClip](https://github.com/wendellrocha/agentclip) 的分支，作者為 [Wendell Rocha](https://github.com/wendellrocha)，依 MIT 授權發布。我們沿用了 Companion 的基礎、SSH 反向通道、基於 stdio 的 MCP 伺服器、在各代理程式中的自動註冊，以及可驗證的發行流程。感謝原始專案。💙

## 授權

依 [MIT 授權](LICENSE)發布。授權同時保留原始 AgentClip 專案的版權，以及 agentport 的版權。
