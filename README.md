# Episteme

可自託管的文件審核與混合檢索工作台。Go 處理文件與索引，Next.js 提供繁體中文管理介面；PostgreSQL 保存原檔、草稿及工作佇列，Qdrant 保存 Dense / Sparse 向量，TEI 提供 embedding 與 reranking。

## 快速開始

Apple Silicon Mac（macOS 15+）可直接安裝原生服務與全部模型：

```sh
./INSTALL.sh
./start.sh
# 檢查 / 停止 / 重啟
./start.sh status
./start.sh stop
./start.sh restart
```

安裝器會安裝缺少的套件、下載並校驗三個模型、建置 API/MCP/前端。啟動器依序啟動 PostgreSQL、Qdrant、Embedding、Reranker、Ollama、API、Web，等待健康檢查成功後才回報完成。開啟 <http://localhost:3000>。首次需要 Xcode 與 Metal 編譯工具；Apple 工具授權或 Homebrew 首次管理員驗證可能需要手動完成。完整版本、資料保存及排錯方式見 [本機安裝指南](docs/local-macos.md)。

### Docker Compose

需要 Docker Compose。預設 TEI CPU 映像適用於 Linux x86_64；Apple Silicon 可將 `.env` 的 `TEI_IMAGE` 設為 `ghcr.io/huggingface/text-embeddings-inference:cpu-arm64-1.9`，或連接另一台 Linux 主機上的 TEI。首次啟動需下載映像與模型；模型快取準備完成後才可離線執行。

```sh
cp .env.example .env
# 編輯 .env，設定 POSTGRES_PASSWORD（建議使用隨機英數字）
docker compose --env-file .env -f deploy/docker-compose.yml up --build -d
# 等待 metadata 服務啟動，再下載一次模型（之後使用 volume 快取）
docker compose --env-file .env -f deploy/docker-compose.yml exec metadata ollama pull gemma2:2b
```

開啟 <http://localhost:3000> 建立管理員。PostgreSQL、Qdrant、TEI 僅在 Compose 內網公開；網頁與 API 預設僅綁 localhost。對外提供服務時，使用 HTTPS 反向代理，設定正確的 `APP_ORIGIN` 及 `COOKIE_SECURE=true`，並先完成管理員建立。

首頁顯示服務實際健康狀態；模型載入中時可能顯示「未連線」。向量容量為 `points × dimension × 4` 的 Dense 資料估算，並非 Qdrant 磁碟實際使用量。

## 日常流程

1. 上傳 Markdown、JSON 或含文字層的 PDF（上限 20 MB）。PDF 由 `pdftotext` 擷取，不含 OCR，也不保證還原複雜多欄表格。JSON 支援物件、陣列及純量；依欄位路徑轉成結構化文字，各陣列項目分開切片，保留欄位順序及數字精度，原始檔可完整下載。
2. 點擊文件進入審核。左側保留原文；右側可編輯內容、章節、FAQ、排除、合併及在游標處切分。
3. 「儲存變更」寫入 PostgreSQL 草稿。按「差異」比較上次入庫與目前內容；所有未排除切片確認後，才能「核可並入庫」。
4. 從索引作業看進度、錯誤與重試狀態。支援暫停／繼續；目前進行中的單一切片會先處理完。
5. 在檢索調試場調整 Alpha / Reranker，查看各路分數與重排名次，從結果回到來源切片。

FAQ 為人工編輯，不呼叫額外生成模型。Markdown 預覽不執行 HTML，且不自動載入外部圖片。

JSON 使用 UTF-8 編碼，每份檔案放一個完整 JSON 值（多筆資料放在陣列中，暫不接受 JSONL）；重複欄位名稱、超過 64 層或 100,000 個值的結構會被拒絕。轉換後的閱讀文字亦以 20 MB 為上限。審核頁顯示結構化閱讀檢視，修改切片不會改寫原始 JSON。

### 文件資料（Metadata）

審核頁上方可展開「文件資料（Metadata）」，填寫標題、來源／機構、來源網址、作者、發布日期、標籤，以及自訂名稱／文字值。這些欄位屬於整份文件，會套用到各個切片；切片 FAQ 維持獨立。

按「儲存變更」可同時保存文件資料與切片草稿，版本衝突時整批取消儲存。修改 metadata 會將所有切片標記為待更新，保留原有確認與排除狀態；「核可並入庫」完成後，搜尋結果的「文件資料」、MCP 搜尋、Qdrant payload 與離線匯出才會帶入新版本。排程中的作業使用送出當時的 metadata，後續草稿不會被該作業提前發布。重新切片會保留文件資料。

目前 metadata 用於來源說明與下游使用，不加入 embedding 文字，也尚未提供依標籤或自訂欄位篩選搜尋。最多 30 個標籤、30 個自訂文字欄位，總計 16 KB；來源網址只接受 HTTP(S)。

### Gemma 自動預填

新上傳的 Markdown、JSON、PDF 會先正常保存原檔和切片，再由背景作業呼叫本機 Ollama 的 `gemma2:2b` 產生 metadata 草稿。工作台顯示「AI 擷取中／已預填／擷取失敗」。請展開文件資料檢查後，再確認切片並入庫；擷取期間暫不接受入庫。

模型擷取標題、來源、來源網址、作者及 ISO 日期，並建議最多六個繁體中文主題標籤。前五項必須逐字出現在送入模型的內容才會保留；這是基本來源檢查，無法保證模型理解的作者／引用對象等角色正確，仍需人工審核。自訂欄位由使用者維護。JSON 先轉為現有結構化閱讀文字，交由模型判讀，不把任意 JSON 欄位直接當成 metadata。

每次使用最多約 4,500 UTF-8 bytes 的文件文字；超過時取開頭 3,000 bytes 和結尾 1,500 bytes，介面標示「節錄」。這是控制 8,192-token 模型 context 的保守上限，長文件中間的來源資料可能不會被擷取。找不到明確日期時留空，不用上傳日期代替。

既有文件不會被背景批次修改，可按「自動補齊空白欄位」（`POST /api/v1/documents/{id}/metadata/extract`）。只填空白欄位，保留既有標籤與自訂欄位；生成期間若 metadata 被人工儲存，整份生成結果不套用。作業寫入 PostgreSQL，服務重啟可復原，模型錯誤最多嘗試三次，失敗後可手動重試，文件及人工編輯仍可使用。

`METADATA_ENABLED` 預設 `true`，`METADATA_OLLAMA_URL` 預設 `http://127.0.0.1:11434`，`METADATA_MODEL` 預設 `gemma2:2b`。若要停用新上傳的自動預填，設 `METADATA_ENABLED=false`；待處理作業會保留至重新啟用。模型僅負責 metadata，不取代 TEI embedding／reranker。macOS 的啟動及模型下載見 [本機服務指南](docs/local-macos.md)。

API 可使用 `PATCH /api/v1/documents/{id}`，傳入 `{ "revision": 目前文件版本, "metadata": { "title": "標題", "tags": ["查核"], "custom": {"語言": "繁體中文"} } }`。`metadata` 為完整替換，空物件可清空；批次切片儲存另接受 `document_revision` 與 `document_metadata`，並與切片編輯使用同一交易。

## 本機開發

Apple Silicon 日常使用請用 [macOS 原生服務指南](docs/local-macos.md) 的 `INSTALL.sh` / `start.sh`。以下為手動開發流程，設定與原生腳本的 `.local/config.json` 分開；同時啟動前須避免 port 衝突。

需要 Go 1.26+、Node.js 24+、PostgreSQL 18，以及 Qdrant 1.17+ / 兩個 TEI 服務。PDF 另需 Poppler (`pdftotext`)。

```sh
cp .env.example .env
# 填入實際服務位置；Go 不會自動載入 .env
set -a
. ./.env
set +a
go run ./cmd/server
```

另一個終端機：

```sh
cd web
npm ci
npm run dev
```

Next.js 會把 `/api/*` 代理至 `http://127.0.0.1:8080`，可用 `API_INTERNAL_URL` 覆寫。請用與 `APP_ORIGIN` 相同的網址進入前端。正式建置採 Webpack，避免部分沙箱的 Turbopack 子程序連接埠限制。

本機正式預覽使用 `cd web && npm run build && npm run start`；啟動脚本會準備 standalone 靜態資源，預設綁定 `127.0.0.1:3000`。

## 索引與一致性

- `bge-m3` 預設為 **1024 維**，並非 768。每次 embedding 都檢查 TEI `/info` 模型識別及向量維度；超過模型限制會回報錯誤，不靜默截斷。
- PostgreSQL 與 Qdrant 不共享 ACID 交易。索引與清理工作使用 PostgreSQL outbox、文件鎖、穩定 point ID、等待寫入完成和冪等重試，達成最終一致性。
- Qdrant 寫入成功後才更新 PostgreSQL 已發布快照。搜尋會核對已發布 revision、模型及排除狀態；不會把尚未核可草稿或刪除的切片當成結果。
- 若服務在向量寫入後、資料庫提交前中斷，搜尋會暫時過濾版本不符的點；重啟後由未完成工作修復。這是可恢復的一致性窗口，不是跨資料庫原子發布。
- 每項工作最多自動重試 5 次（遞增退避）；之後顯示失敗，可手動繼續。重切或刪除先更新 SQL，向量清理由 outbox 完成。不要取消尚未完成的清理作業，否則實體向量會保留到重試。
- Sparse 使用穩定詞彙雜湊、英文詞與中文單字／雙字詞，以及 BM25 TF 飽和（`k1=1.2, b=.75`、固定參考長度 256）；IDF 由 Qdrant 計算。這不是 BGE-M3 learned sparse，也不是精確依全庫平均長度重新校正的 BM25。
- Alpha 採加權 Reciprocal Rank Fusion，`alpha/(60+dense_rank) + (1-alpha)/(60+sparse_rank)`。`alpha=0` 完全不呼叫 embedding；`alpha=1` 不查 Sparse。兩路各取 100 個候選，過濾後取預設 Top-20 重排。
- 預設只回傳完整命中查詢詞或達到相關性門檻的結果，再合併同一文件中的相同段落，不會為了湊滿 20 筆保留弱相關內容。`min_score` 預設為重排 0.1、停用重排時 Dense 0.5；這是可調整的篩選值，不代表機率或通用的模型品質保證。短關鍵字的重排分數可能偏低，因此完整文字命中仍保留；多個以空白分隔的查詢詞需全部命中。純 BM25 且停用重排時採完整文字命中。
- 調試場可調整分數門檻，或開啟「顯示全部候選」（API/MCP：`include_low_relevance=true`）比較篩選前候選。篩選只作用於已發布結果，不會修改切片、排除狀態或重建索引。
- Token 數是估算值；切片長度是目標值。表格、程式碼與清單保持完整，超長區塊由介面警示，需人工切分。
- 手動 split 的原文定位繼承原區塊範圍，merge 合併範圍；不虛構人工改寫後的精確字元對應。

### 更換 embedding 模型

先暫停／取消既有索引作業，修改 TEI 模型、`EMBEDDING_MODEL` 和 `EMBEDDING_DIMENSION`，重啟 API，再按「全庫重建」。模型與維度的雜湊構成 collection 名稱，避免不同空間混用。切換期間只有新模型已完成的內容會被檢索；這不是零停機切換。舊 collection 保留供維運確認後刪除；文件刪除會排程清理該文件曾使用的 collection。

## MCP

使用官方 Go MCP SDK，提供 `search_knowledge` / `list_documents` tools、`episteme://documents` resource 與 `episteme://documents/{id}` 原文 resource template。MCP 是整個工作台的可信讀取入口，不提供匿名存取或細分使用者 ACL。

Stdio：

```sh
go build -o bin/mcp-stdio ./cmd/mcp-stdio
```

Claude Desktop / Cursor 設定範例（環境變數需指向你的服務）：

```json
{"mcpServers":{"episteme":{"command":"/absolute/path/Episteme/bin/mcp-stdio","env":{"DATABASE_URL":"postgres://...","QDRANT_HOST":"localhost","TEI_EMBED_URL":"http://localhost:8081","TEI_RERANK_URL":"http://localhost:8082"}}}}
```

Stdio 直接使用設定的資料庫權限，只供可信任的本機使用者。HTTP 設定隨機 `MCP_TOKEN` 後使用 `Authorization: Bearer ...`：

- `http://localhost:8080/mcp`：Streamable HTTP。
- `http://localhost:8080/mcp/sse`：舊版 SSE；POST 使用 SSE 回傳的 endpoint。

空白 `MCP_TOKEN` 會關閉 HTTP MCP。請勿把 token 寫入 Git 或 URL。

## 離線匯出

```sh
go run ./cmd/export > published-chunks.jsonl
```

只匯出已發布且未排除的內容，使用 Vertex AI Search 結構化 JSONL 的 `id` / `jsonData` 格式。目的 data store 需設定對應欄位 schema。此指令不會讀取雲端憑證、上傳或建立雲端資源；雲端匯入未在本機驗證。

## 驗證

```sh
go test ./...
TEST_DATABASE_URL='postgres://.../episteme_test?sslmode=disable' go test -race -count=1 ./...
cd web && npm run build
```

整合測試在指定 PostgreSQL 建立隨機 schema，結束後清除；未設定 `TEST_DATABASE_URL` 時明確跳過資料庫測試。TEI HTTP 與 Qdrant 使用測試替身，因此通過不等同真實模型效果或 Docker 全服務驗收。詳見 [`docs/verification.md`](docs/verification.md)。`make generate` 使用固定版本 sqlc 重新產生查詢；已產生程式碼納入版控。

## 結構

`cmd/`：API、Stdio、離線匯出；`internal/`：auth、chunker、api、indexer、retrieval、inference、storage、mcp、cloud；`sql/`：schema / sqlc；`web/`：Next.js App Router、TanStack Query / Virtual、CodeMirror、Tailwind / shadcn 風格元件；`deploy/`：容器與 Compose。

目前採單一管理員工作台及逐切片 worker；不包含多租戶 ACL、OCR、自動生成 FAQ、零停機模型切換或跨服務 ACID。正式使用前應依文件與模型規模測量吞吐、備份恢復及記憶體需求。

## 參考

- [BGE-M3 模型規格](https://huggingface.co/BAAI/bge-m3)
- [Qdrant 混合檢索](https://qdrant.tech/documentation/search/hybrid-queries/)
- [TEI 啟動與推論](https://huggingface.co/docs/text-embeddings-inference/quick_tour)
- [TEI 各硬體對應映像](https://huggingface.co/docs/text-embeddings-inference/supported_models)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [Vertex AI Search 資料準備](https://docs.cloud.google.com/generative-ai-app-builder/docs/prepare-data)
