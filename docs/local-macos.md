# macOS 本機安裝與啟動

適用於 **Apple Silicon、macOS 15 以上**，使用 TEI Metal 與 Ollama Metal。Linux、Intel Mac、Windows 及 Rosetta 不在這兩個原生腳本的支援範圍；其他環境請使用 README 的 Docker Compose 流程。

## 第一次安裝

在專案根目錄執行，或以腳本的絕對路徑呼叫：

```sh
./INSTALL.sh
./start.sh
```

開啟 <http://localhost:3000> 建立管理員。安裝不會建立示範文件或預設管理員密碼。MCP 的 HTTP 服務預設停用；需要時在 `.local/config.json` 設定隨機 `mcp_token` 後重新啟動 API。

`INSTALL.sh` 會：

1. 檢查原生 ARM64、Xcode 與 Metal 編譯器。缺少 Command Line Tools 時啟動 Apple 安裝提示；有 Xcode 但缺少 Metal 元件時嘗試 `xcodebuild -downloadComponent MetalToolchain`。完整 Xcode 的下載、授權及系統安裝視窗仍須由使用者完成，再重跑腳本。
2. 缺少 Homebrew 時執行官方安裝器，可能要求管理員密碼。請以一般使用者執行整個腳本，不要 `sudo ./INSTALL.sh`。
3. 安裝 PostgreSQL 18、Poppler（PDF 擷取）、CMake、pkgconf、Protobuf、OpenSSL 3、Rustup；不執行全機 `brew upgrade` 或註冊 Homebrew 常駐服務。Homebrew 安裝缺少套件時仍可能更新其相依套件。
4. 將固定版 Go、Node、Qdrant、Ollama 放在 `.local/`，不覆寫系統 Go/Node/Ollama。TEI 以固定 Rust toolchain、固定原始碼 commit 與 Cargo.lock 建置 Metal 版本；已有同版 Metal 執行檔時沿用。
5. 預先下載並校驗 BGE-M3、BGE Reranker、Gemma 2 2B，沿用已驗證的快取。安裝模型不需要先啟動任何服務。
6. 使用 `go.mod` / `go.sum` 與 `npm ci` 建置三個 Go 指令和 Next.js standalone 前端。建立權限 `0600` 的 `.local/config.json`，保存隨機資料庫密碼；已有設定時保留。

模型合計約 6.2 GB；首次編譯另需 Go/Node/Rust 快取與 Xcode 空間，建議開始前預留至少 20 GB 可用磁碟空間、16 GB 記憶體。這是容量建議，不是所有文件負載的效能保證。下載中斷可重跑；模型支援續傳且完成後校驗內容。Homebrew 的短暫下載失敗會重試。若更新程式，先 `./start.sh stop`，再重跑 `./INSTALL.sh`，最後 `./start.sh`。

## 版本策略

固定版本集中在 `scripts/versions.env`，模型逐檔大小與雜湊在 `scripts/models.lock.json`：

| 元件 | 固定／相容版本 |
| --- | --- |
| Go | 1.26.5，`GOTOOLCHAIN=local`，不自動換編譯器 |
| Node.js | 24.14.0；npm 使用該官方發行檔附帶版本 |
| Web 套件 | `web/package-lock.json`，使用 `npm ci` |
| PostgreSQL | Homebrew `postgresql@18`；允許同一 major 的修補版，檢查 `PG_VERSION`，不自動跨 major 升級 |
| Qdrant | 1.17.0 |
| TEI | 1.9.0，commit `5699247f57e46aa09eb4f8c4cf74114099372fe7` |
| Rust / metrics | 1.92.0 / 0.23.1，透過已納管的小型 Cargo.lock patch 修正 E0521，不重新解析其餘依賴 |
| Ollama | 0.5.7，專案私有的官方 macOS 發行檔 |
| BGE-M3 | `5617a9f61b028005a4858fdac845db406aefb181`，1024 維 |
| BGE Reranker v2 M3 | `953dc6f6f85a1b2dbfca4c34a2796e7dde08d41e` |
| Gemma 2 2B | Q4_0，manifest SHA-256 `8ccf136fdd5298f3ffe2d69862750ea7fb56555fa4d5b18c04e3fa4d82ee09d7` |

Go、Node、Qdrant、Ollama 官方壓縮檔以固定 SHA-256 校驗；大型模型權重以 SHA-256、小型 Git 檔以 Git blob SHA-1 校驗。Gemma 下載固定 manifest 指定的 blobs，不以可漂移的 `ollama pull ...:latest` 決定權重。

Poppler 與建置輔助工具採 Homebrew 的相容套件，不宣稱完全固定每個 transitive system library；安裝時的實際版本記錄在 `.local/installed-brew-versions.txt`。首次安裝仍需要連線官方下載來源。這組版本以相容性為目的固定，未來更新須一起驗證，腳本不自動跟隨 upstream latest。

## 日常操作

```sh
./start.sh              # 同 ./start.sh start，背景啟動全部服務
./start.sh status       # 非全部 ready 時回傳非零 exit code
./start.sh stop         # 反向順序停止；保留資料與模型
./start.sh restart
./start.sh run          # 前景監督，Ctrl+C 停止所選服務
./INSTALL.sh --check    # 檢查安裝、版本、模型檔與前端代理設定
./INSTALL.sh --models-only  # 只補齊／校驗三個固定模型；先停止服務
```

安裝檢查不代表服務已在執行；`status` 才檢查受管理程序和實際健康。啟動會驗證模型名稱/revision 與 Ollama 版本，TEI 載入最多等待 300 秒，其餘服務最多 60 秒。失敗會回報對應日誌，並停止本次新啟動的程序；已在執行的受管理程序會保留。Next.js 收到停止訊號後先正常關閉，最多等待 10 秒；其餘程序最多等待 30 秒，逾時保留 PID 記錄供排查。重跑 `start` 不重複啟動。

預設啟動順序：PostgreSQL → Qdrant → Embedding → Reranker → Ollama → API → Web。Go API 自動套用 schema 並執行 indexing / metadata 背景工作；HTTP MCP 也由 API 提供，不需要多開 MCP daemon。Stdio MCP 由連接的客戶端按需啟動 `bin/mcp-stdio`。

維運時可指定服務，例如 `./start.sh restart api web`。指定子集合時不會自動補啟相依服務；日常直接使用完整 `./start.sh`。相容入口 `scripts/local-services.sh` 操作 Qdrant / Embedding / Reranker，`scripts/local-metadata.sh` 操作 Ollama，兩者都共用新管理器。

所有服務僅綁定 `127.0.0.1`，不註冊系統開機啟動。一般 Terminal 的背景啟動可在命令結束後繼續；會主動回收子程序的工具環境請保持 `./start.sh run` 工作階段。

| 服務 | 預設連接埠 | 檢查 |
| --- | --- | --- |
| PostgreSQL | 55439 | `pg_isready`、建立資料庫時驗證登入 |
| Qdrant HTTP / gRPC | 6333 / 6334 | `/healthz` |
| Embedding | 8081 | `/health`、`/info` |
| Reranker | 8082 | `/health`、`/info` |
| Ollama | 11434 | `/api/tags` 確認固定 Gemma digest、`/api/version` |
| Go API / HTTP MCP | 8080 | `/healthz` 確認 DB 連線 |
| Next.js | 3000 | `/api/v1/auth/me` 確認前端代理能連到 API |

## 設定與資料保存

原生腳本使用 `.local/config.json`，**不載入 Compose 的 `.env`**，避免誤接到不同資料庫或模型。可在停止服務後修改 `postgres_port`、`postgres_data`、資料庫帳號／密碼、`mcp_token`。`app_origin` 允許 `http://localhost:3000` 或 `http://127.0.0.1:3000`，瀏覽器需使用相同來源。API 8080 與 Web 3000 為此原生組合固定 port，因 Next.js rewrite 在建置時決定。

初次安裝可用 `EPISTEME_PG_PORT=55440 ./INSTALL.sh` 避開另一個 PostgreSQL；已有 config 時該變數不覆寫設定。已有 PostgreSQL 帳號／密碼不會被安裝器重設，修改 config 前需先在資料庫調整對應權限。可用絕對路徑 `EPISTEME_RUNTIME_DIR` 指定 runtime，但 INSTALL 和 start 必須使用一致設定，且同一 checkout 的 Go/前端建置仍共用。

- `.local/postgres`：持久化 PostgreSQL，含原檔、切片、工作佇列、帳號。
- `.local/qdrant`：向量與快照。
- `.local/models` / `.local/model-links`：固定 revision 的 BGE 檔與本機路徑；TEI 從本機載入，不在啟動時查 Hub。
- `.local/ollama/models`：Gemma manifest 與權重。
- `.local/tools` / `.local/bin` / `.local/build`：私有工具及建置快取。
- `.local/logs` / `.local/pids`：日誌及 PID、程序開始時間記錄。

首次啟動才初始化 PostgreSQL，使用 SCRAM 密碼與 loopback TCP，不建立共享 `/tmp` socket。非空但缺少 `PG_VERSION` 的資料目錄、major 不符或 port 被不屬於此管理器的程序占用，都會中止；不刪資料、不接管或 kill 其他服務。停止依 PID **及開始時間** 辨識，避免誤殺 PID 重用後的程序。安裝和啟停有互斥鎖，防止同時操作。

如果從早期手動流程轉換，先停止舊 API/Web/TEI/Qdrant/Ollama；舊的 `.pid` 檔不會當成新管理器的授權記錄。原先用 `/tmp` 的 PostgreSQL 不會自動搬移；請先備份並用 `pg_dump` / `pg_restore` 匯入持久化資料庫，或明確設定既有資料目錄。不要透過初始化覆蓋舊資料。

`.local/`、`bin/`、`.env`、模型及憑證已排除於 Git / Docker context。**刪除 `.local/` 會刪掉本機資料庫與模型**；停止或重新安裝不需要刪除此目錄。

## 排錯與驗證

```sh
./start.sh status
tail -n 80 .local/logs/embedding.log
tail -n 80 .local/logs/metadata.log
make test-scripts
python3 scripts/download-models.py --check  # 全量雜湊校驗，會讀取約 6.2 GB
```

`connection refused` 代表指定 port 沒有服務，只改 IPv4/IPv6 位址不足以解決。TEI concurrency 64 可容納 Top-20 重排，實際 batch 為 4 筆／8192 tokens；BGE-M3 使用 PyTorch 權重是已知模型格式，不是下載錯誤。模型量化、來源品質及 metadata 正確性仍需應用層審核。

來源：[TEI 1.9 官方本機安裝](https://github.com/huggingface/text-embeddings-inference/tree/v1.9.0#local-install)、[Qdrant 1.17.0 發行](https://github.com/qdrant/qdrant/releases/tag/v1.17.0)、[Ollama 0.5.7 發行](https://github.com/ollama/ollama/releases/tag/v0.5.7)、[Homebrew 安裝](https://docs.brew.sh/Installation)、[PostgreSQL 18](https://www.postgresql.org/docs/18/)。
