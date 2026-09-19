# 驗證紀錄

日期：2026-09-19。環境：macOS arm64、Go 1.26.5、Node.js 26.7.0、PostgreSQL 18。

下列示範檔名為歷史驗證紀錄；完成驗證後，示範原檔與本機上傳資料已依使用者要求移除，不隨專案發佈。程式內的自動化測試使用隔離資料，仍保留供回歸驗證。

## 已完成

- `go test -race -count=1 ./...`，包含真實 PostgreSQL 隔離 schema 的完整文件生命週期測試。
- `go vet ./...`。
- `npm run build`（Next.js 16.3.5 / Webpack），包含 TypeScript 檢查。
- sqlc 1.30.0 產生查詢與編譯。
- Markdown AST、Unicode split、表格／程式碼保留、原文內容完整性。
- HTTP 整合：首次建立管理員、登入／登出、未登入拒絕、跨來源拒絕、上傳、草稿重讀、409 版本衝突、未審核阻擋入庫。
- 索引整合：發布後檢索、未發布草稿不外洩、單片更新不重算其他切片、排除後立即過濾、索引失敗重試、暫停／繼續、重切清理、文件刪除。
- Qdrant gRPC 協定測試：collection 配置、具名向量、中文章節 payload、revision、等待 Upsert / Delete 完成。
- 官方 MCP SDK：記憶體管道（與 Stdio 相同 JSON-RPC 層）、Streamable HTTP、SSE 初始化、tools/list、resources/list。
- 瀏覽器正式建置：登入、Markdown 上傳、5 個切片、CodeMirror 修改、儲存草稿、重新載入仍保留內容、全部確認、再次儲存、提交索引後出現作業。
- 原文定位：選取「草稿與發布」切片後，原文相應標題高亮，原文捲動區同步定位。
- 響應式版面：1440px 桌面雙欄、820px 平板與 390px 手機單欄切換；原文表格可閱讀，底部儲存／入庫操作列保持可用。
- `bin/server`、`bin/mcp-stdio`、`bin/export` 實際編譯完成；API binary 與 Next.js standalone 正式啟動腳本均已啟動本機預覽。
- 實際 Stdio 子程序：JSON-RPC initialize、tools/list、list_documents 呼叫均成功，並連接本機真實資料庫。
- 瀏覽器章節編輯：逐字輸入 `Episteme / Review` 可保留分隔符號、更新兩層章節並要求重新確認；恢復示範內容後儲存成功。
- 檢索調試場：推論服務離線時顯示 TEI 連線錯誤，不回傳虛構結果。

## 驗證邊界

- 資料庫及 API 流程使用真實 PostgreSQL；TEI HTTP 與 Qdrant 的後端測試使用協定替身，不代表神經模型效果或真實 Qdrant 搜尋品質已驗收。
- 初次本機預覽未啟動 TEI／Qdrant；後續已安裝原生服務並完成實際推論，見下方追加驗證。原先的失敗作業歷史仍保留。
- 本機無 Docker，因此尚未執行 Compose 全服務啟動、容器映像建置、Linux CUDA 推論、實體向量磁碟量、長時間吞吐或服務重啟壓力測試。
- 本機未安裝 `pdftotext`；PDF 解析路徑已實作並加入容器依賴，但尚未以實際 PDF 在此環境完成驗證。
- Stdio binary 已實測 JSON-RPC；尚未由實際 Claude Desktop／Cursor 設定接入。
- 離線 JSONL 匯出可編譯；未提交 Vertex AI 匯入、建立雲端資源或公開部署。

## macOS 原生服務追加驗證

- Qdrant 1.17.0 官方 Apple Silicon 執行檔，已校驗 SHA-256；HTTP 6333、gRPC 6334 均啟動。
- TEI 1.9.0 使用 Metal 建置。上游 Cargo.lock 的 `metrics 0.23.0` 在 Rust 1.92 / 1.97 皆出現 E0521；升至官方相容修正版 0.23.1 後建置成功，模型與 API 程式碼未改動。
- Embedding `localhost:8081/info` 實際回報 `BAAI/bge-m3`，中文輸入回傳 1024 維有限數值，向量 norm 為 1.0。
- Reranker `127.0.0.1:8082/rerank` 實際推論：相關答案分數約 0.899，無關番茄段落約 0.000017。這是功能檢查，並非一般化檢索品質評測。
- 前端 Dashboard 三個服務健康狀態皆為「正常」。本機程序只監聽 loopback；啟停與重開機後操作見 [macOS 本機指南](local-macos.md)。
- 從瀏覽器重試 `knowledge-guide.md` 的失敗作業，進度完成 5/5；PostgreSQL 文件狀態為 `indexed`，五個切片的 `indexed_revision` 都等於目前 `revision`，且 `is_dirty=false`。
- 瀏覽器查詢「草稿什麼時候可以被搜尋？」（Alpha 0.5、Reranker 開啟），真實 TEI + Qdrant 管線回傳 5 個結果，顯示 Dense、BM25、融合、重排分數和來源連結；當次約 3.9 秒。
- 補上輪詢取得索引進度時同步刷新文件／切片快取，避免 SSE 未更新時仍顯示舊失敗狀態；Next.js 正式建置與 TypeScript 檢查通過。

## JSON 上傳追加驗證

- `go test -race -count=1 ./...` 再次通過，使用本機 PostgreSQL 隔離 schema；`go vet ./internal/ingest ./internal/api`、Next.js 正式建置與 TypeScript 檢查通過。
- JSON 單元測試涵蓋巢狀路徑、陣列分筆、欄位順序、`9007199254740993` 與小數精度、中文長文分片、純量／空容器、UTF-8 BOM、Markdown 注入防護、格式錯誤、重複欄位、結構深度及值數量限制。
- HTTP 整合驗證 `.JSON` 副檔名、錯誤輸入不寫入資料庫、`application/json` MIME、原檔逐位元組一致（含 BOM／空白）、審核入庫、檢索與重切後維持資料分筆。
- 瀏覽器上傳 `examples/knowledge-guide.json` 成功，產生 4 片；文件庫顯示 JSON，審核頁顯示 `JSON/條目/0` 等欄位路徑及結構化原文提示。
- 瀏覽器全部確認、儲存、核可並入庫完成 4/4；實際 TEI embedding 與 Qdrant 索引成功。
- 新增資料後的 9 筆搜尋候選揭露本機 Reranker 的併發上限 8 過低（HTTP 429）；啟動腳本調為 64 並重啟 Reranker。模型批次上限仍為 4，不增加單批處理量。
- 瀏覽器查詢「如何取得上傳時的原始 JSON 檔案？」（Alpha 0.5、Reranker 開啟），回傳 9 筆結果；第一筆為 JSON 的「原始 JSON 保存」，重排分數約 0.993，當次約 3.2 秒。

## 搜尋相關性追加驗證

- 發現原本只取 Top-20 並重排，沒有相關性門檻；接近零分的候選及 JSON 摘要／內文重複段落仍會列出。
- 新增可調整的 `min_score` 與 `include_low_relevance`。預設採重排 0.1／停用重排時 Dense 0.5，完整查詢詞命中可保留；分數是篩選值，不是校準後的機率。JSON 產生的路徑不視為正文命中，同文件重複段落只保留排名較高的一筆。
- 回歸測試涵蓋短中文詞、英文詞邊界、多詞全部命中、語意召回、可調門檻、零結果、不同文件來源保留、候選調試模式及排名編號。API 整合確認調試模式仍遵守已發布／排除規則，非法門檻回 400，`limit=1` 在重排之後才套用。
- `go test -race -count=1 ./internal/api ./internal/retrieval ./internal/mcp` 全部通過。受影響套件 `go vet`、Next.js 正式建置及 TypeScript 檢查通過。
- 瀏覽器實際搜尋「候選人」（Alpha 0.5、重排啟用）從 20 筆候選篩為 5 筆完整關鍵字命中，重複背景段落／摘要合併。開啟「顯示全部候選」仍可查看 20 筆及低相關標記，關閉後恢復篩選。
- 自然語句「如何取得上傳時的原始 JSON 檔案？」保留 1 筆正確的原檔下載說明，標為語意相關，重排約 0.993。無關查詢「量子香蕉火箭」回傳 0 筆並顯示調整提示。
- 已檢視桌面雙欄與目前 753px 窄版的新增門檻／候選開關。現有文件、草稿及向量沒有為此修正而重切或重建。

## 文件 Metadata 追加驗證

- `go test -race -count=1 ./...` 通過，包含真實 PostgreSQL 隔離 schema 的文件生命週期；`go vet ./...`、三個 Go 執行檔建置、Next.js 正式建置與 TypeScript 檢查通過。
- 驗證 metadata 正規化、日期／網址／大小限制、標籤去重、自訂名稱衝突、文件版本 409、與切片共同儲存的原子性，以及清空欄位、重切保留。
- 發布測試：A 版送出索引後再儲存 B 版草稿，舊作業只發布 A 版且保留待更新狀態；再次提交才發布 B 版。搜尋、向量 payload 和離線匯出使用已核可快照。Qdrant 協定測試確認 metadata 為巢狀欄位，不會覆蓋保留的 document_id。
- 本機套用新增欄位的 migration，原有文件／切片筆數維持 15／351；未替使用者文件填入 metadata。
- 瀏覽器對示範 `knowledge-guide.json` 填寫標題、來源、作者、日期、標籤和「語言」自訂欄位，儲存並重新載入後保留。metadata 編輯不改變原有 4／4 已確認狀態。
- 檢查桌面雙欄欄位與 390px 手機單欄表單、自訂欄位及底部操作列。內嵌瀏覽器的原生日期彈出選擇器曾造成頁面中斷，改為 YYYY-MM-DD 文字輸入並由後端驗證，實際保存與重新載入成功。
- 瀏覽器送出入庫後，真實 TEI + Qdrant 完成 4／4；資料庫四片的已發布 metadata 與草稿一致，`is_dirty=false`。實際 `bin/export` 產生的四筆示範資料也包含相同 metadata。
- 實際搜尋「如何取得上傳時的原始 JSON 檔案？」回傳 1 筆相關內容，重排約 0.993；展開結果中的「文件資料」可讀取標題、來源、作者、2026-09-19 日期、示範／JSON 標籤與「語言：繁體中文」，並已檢視完整卡片排版。

## Gemma 自動 Metadata 追加驗證（2026-09-19 至 2026-09-20）

- 使用本機已安裝的 Ollama 0.5.7 + Metal，下載並完成 SHA-256 驗證的 `gemma2:2b`（Q4_0，約 1.6 GB），只綁定 127.0.0.1:11434；`scripts/local-metadata.sh status` 確認模型 ready。
- 全部 Go race 測試、`go vet ./...`、Go 執行檔建置、Next.js 正式建置及 TypeScript 檢查通過。最後補上 metadata 合併超過 16 KB 的失敗狀態後，再跑 API／metadata race 測試與 API 建置通過。
- 真實 PostgreSQL 隔離 schema 驗證：上傳與背景作業同交易、重複排程 409、擷取期間阻擋入庫、完成後保持草稿和待審核、只填空白欄位、保留人工標籤及自訂欄位、擷取期間清空欄位不被覆蓋、最多三次嘗試、手動重試、過期 lease 復原、舊 worker 結果不覆蓋新 claim、文件刪除取消作業、合併超限不破壞草稿且不陷入無限重試。
- 模型 HTTP 契約測試檢查結構化 JSON、8192 context、非串流及取消；對原文不存在的作者、網址、日期清空，對格式錯誤／截斷回應回報失敗；4500-byte 首尾節錄維持 UTF-8 完整。這些規則只做基本來源檢查，不能證明模型對引文／作者等角色的判斷正確。
- 瀏覽器上傳 `examples/metadata-demo.md` 後顯示 AI 排程中，自動預填標題、來源、來源網址、作者、日期、六個繁體中文標籤；三片仍為 0／3 待確認，未自動發布。重新載入後資料保留。
- 人工新增「用途：自動擷取驗證」並儲存，按「自動補齊空白欄位」後顯示擷取狀態且欄位暫時停用；真實模型約 3 秒完成再次擷取，人工「用途」欄位仍保留。已檢視桌面與 390px 手機版狀態區；首頁四個服務包含 Metadata AI 皆正常。
- 原有 15 份文件沒有被批次擷取，僅新增上述示範文件。Docker metadata 服務配置及實際 PDF 擷取仍未在本機驗證；模型繁體中文品質未做大規模評測，長文件只用首尾節錄。

## 提交前清理（2026-09-20）

- 依使用者要求，先移除三份示範文件，再移除其餘十三份上傳文件，均透過正常刪除 API 排程向量清理。
- 十六個刪除作業全部完成，Qdrant collection 精確計數為 0；PostgreSQL 文件、切片、metadata 作業、索引作業及索引 task 均為 0。
- 移除專案中的三個示範原檔並更新說明；保留回歸測試。模型、資料庫、向量儲存、編譯產物與個人環境設定不納入 Git。
- 清理後再次通過完整 Go race 測試、`go vet ./...` 與前端 TypeScript 檢查。資料庫測試使用隔離 schema，測試結束清除。

## 完整服務驗收方式

1. 設定 `.env`，以 Compose 啟動，等待首頁三個服務皆正常。
2. 準備專用驗收用 Markdown、JSON 及一份有文字層的 PDF，上傳後確認原檔下載與切片內容。
3. 校對、儲存、重新載入、確認入庫；等待作業完成，再查詢「草稿什麼時候可以被搜尋」。
4. 修改單片，先驗證仍回傳上次發布版本；局部更新後確認新內容被召回。
5. 排除、合併、切分、重切與刪除，驗證結果不包含舊切片。
6. 停止 Qdrant，再提交作業；重啟 Qdrant、繼續作業，驗證持久化重試。
7. 暫停全庫重建、重啟 API、繼續作業，確認進度延續。
8. 配置 MCP bearer token 或 Stdio binary，實際呼叫 tools 與原文 resource。
