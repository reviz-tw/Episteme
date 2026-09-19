# macOS 本機模型與向量服務

適用於 Apple Silicon。使用原生 Qdrant 與 Hugging Face TEI Metal 執行檔，模型推論在本機進行。首次需連線下載公開模型與建置依賴；準備數 GB 磁碟空間。

## 安裝

需要 Rustup 及 Xcode 的 Metal 編譯工具（`xcrun -f metal` 應能找到工具）。使用 TEI 指定的 Rust 1.92，並將其舊鎖檔中的 `metrics` 升至官方相容修正版 0.23.1，處理 `E0521` 編譯錯誤。從專案根目錄執行：

```sh
mkdir -p .local/bin
curl --fail --location \
  https://github.com/qdrant/qdrant/releases/download/v1.17.0/qdrant-aarch64-apple-darwin.tar.gz \
  --output /tmp/episteme-qdrant.tar.gz
tar -xzf /tmp/episteme-qdrant.tar.gz -C .local/bin

git clone --depth 1 --branch v1.9.0 \
  https://github.com/huggingface/text-embeddings-inference.git /tmp/episteme-tei
rustup toolchain install 1.92.0 --profile minimal
rustup run 1.92.0 cargo update --manifest-path /tmp/episteme-tei/Cargo.toml \
  -p metrics --precise 0.23.1
CARGO_TARGET_DIR=/tmp/episteme-tei-target rustup run 1.92.0 cargo install --locked \
  --path /tmp/episteme-tei/router --features metal --root "$PWD/.local" -j 6
```

Qdrant 發行頁 API 的 asset `digest` 可供 SHA-256 核對；目前使用的 Apple Silicon 1.17.0 壓縮檔為 `43feed023b4737a509f81dbe695afbb355346a4884482fe1026d23f4d68ab183`。

## 啟動與檢查

```sh
./scripts/local-services.sh start
./scripts/local-services.sh status
```

若終端機工具會在命令結束時回收子程序，改用 `./scripts/local-services.sh run` 保持前景工作階段；按 Ctrl+C 可停止這組服務。

首次啟動 TEI 會下載 `BAAI/bge-m3` 與 `BAAI/bge-reranker-v2-m3`。`starting` 表示程序仍在執行但尚未健康，需查看 `.local/logs/embedding.log` 或 `.local/logs/reranker.log`；只有 `ready` 才代表服務可用。

TEI 的 `max-concurrent-requests` 設為 64，容納預設 Top-20 重排候選；TEI 重排會為每個候選占用一個名額，設為 8 時，單次搜尋超過 8 筆也會回 HTTP 429。實際模型批次仍限制為 4 筆／8,192 tokens，以控制 Metal 記憶體用量。

本次模型快取約 4.6 GB。BGE-M3 官方 repository 使用 `pytorch_model.bin`，TEI 會在找不到 safetensors 後讀取該權重；這個 fallback 訊息本身不代表啟動失敗。Embedding revision 為 `5617a9f61b028005a4858fdac845db406aefb181`，reranker revision 為 `953dc6f6f85a1b2dbfca4c34a2796e7dde08d41e`。

| 服務 | 本機位址 | 健康檢查 |
| --- | --- | --- |
| Embedding | `127.0.0.1:8081` | `/health` |
| Reranker | `127.0.0.1:8082` | `/health` |
| Qdrant HTTP | `127.0.0.1:6333` | `/healthz` |
| Qdrant gRPC | `127.0.0.1:6334` | API 使用此 port |

Go API 的設定：

```sh
export TEI_EMBED_URL=http://127.0.0.1:8081
export TEI_RERANK_URL=http://127.0.0.1:8082
export QDRANT_HOST=127.0.0.1
export QDRANT_GRPC_PORT=6334
export EMBEDDING_MODEL=BAAI/bge-m3
export EMBEDDING_DIMENSION=1024
# 保留原本的 DATABASE_URL / APP_ORIGIN，啟動 API 與 web。
```

三個服務正常後，回工作台對已確認文件按「核可並入庫」，或對先前失敗作業按「繼續作業」。待作業完成才會有可搜尋內容。

## Gemma 2 2B 自動 Metadata

這台 Mac 已安裝 Ollama 0.5.7，使用其中的 CLI 與 Metal 執行 `gemma2:2b`。模型為 Ollama library 的 Q4_0 版本，下載約 1.6 GB，存於專案 `.local/ollama/models`。此服務獨立於 TEI，不會更換既有搜尋模型。

```sh
./scripts/local-metadata.sh start
./scripts/local-metadata.sh pull     # 首次下載；之後使用快取
./scripts/local-metadata.sh status
```

若命令工具會回收背景子程序，使用 `./scripts/local-metadata.sh run` 保持前景。腳本優先使用 `/Applications/Ollama.app/Contents/Resources/ollama`，也可用 `OLLAMA_BIN` 指定路徑；僅監聽 `127.0.0.1:11434`，不修改已占用該 port 的服務。`status` 會確認模型已下載。

API 預設啟用 `METADATA_ENABLED=true`、`METADATA_OLLAMA_URL=http://127.0.0.1:11434`、`METADATA_MODEL=gemma2:2b`。新文件上傳後排程擷取，首頁 `Metadata AI` 正常表示服務可連線且模型已存在，不等同每份文件擷取品質已驗收。API 不會自動下載或更換模型。原生服務可使用 Metal；Compose 的 metadata 服務目前為 CPU 配置，未在本機實跑容器。

停止：`./scripts/local-metadata.sh stop`。不刪除模型、原檔或草稿；重開機後需再啟動。套件與模型資訊：[Ollama Gemma 2](https://ollama.com/library/gemma2:2b)、[結構化輸出](https://docs.ollama.com/capabilities/structured-outputs)。

## 停止與資料保存

```sh
./scripts/local-services.sh stop
```

此腳本只停止由自己記錄且執行檔吻合的程序，不刪除模型或向量資料。全部服務僅綁定 `127.0.0.1`；Qdrant telemetry 關閉。`.local/` 保存執行檔、模型快取、向量資料與日誌，已排除於 Git 及 Docker build context。重開機後需重新執行 `start`；腳本不會加入系統開機常駐服務。

`connect: connection refused` 表示該位址沒有可接受連線的服務。只將 `localhost` 改成 `127.0.0.1` 無法解決服務未啟動；先用 `status` 和日誌檢查。CPU 版 Docker Compose 與這套原生服務使用不同向量資料目錄，切換前請安排備份或重建。

依據：[TEI 官方 Local install](https://github.com/huggingface/text-embeddings-inference/tree/v1.9.0#local-install)、[Qdrant 官方安裝文件](https://qdrant.tech/documentation/installation/)。
