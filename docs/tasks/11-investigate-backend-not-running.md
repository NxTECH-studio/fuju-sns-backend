# 11 - Investigate Backend Not Running (`localhost:8082` ERR_CONNECTION_REFUSED)

## 概要

フロントエンドから `http://localhost:8082` のバックエンド API へのすべてのリクエストが `ERR_CONNECTION_REFUSED` になる事象の原因を特定し、恒久対処する。調査の主仮説は `docker-compose.yml` の `DB_PORT: 5433` 誤設定（postgres コンテナは `5432` で listen）により backend が DB 接続失敗で起動できず、ポート `8082` に誰も listen していない状態になっていること。

## 前提（依存タスク）

- `10-cookie-session-handoff.md`（Phase 1 実装済み、Phase 2 作業中の `feat/cookie-session-handoff-phase2` ブランチ上で発覚した症状）
- このタスクは Phase 2 実装そのものではなく、**ローカル開発環境が起動しない問題の切り分け**。Phase 2 PR と分けて扱う（`docker-compose.yml` / `.env.example` の修正は小さい独立 PR で出せる想定）。

## 背景・目的

### 観測された症状（ブラウザコンソール）

`ERR_CONNECTION_REFUSED` になっているリクエスト:

- `GET  http://localhost:8082/timeline/home?limit=20`
- `GET  http://localhost:8082/timeline/global?limit=20`
- `GET  http://localhost:8082/me`
- `GET  http://localhost:8082/v1/images`
- `POST http://localhost:8082/posts`

一方、外部 AuthCore は正常:

- `POST https://auth.sheeplab.net/v1/auth/login` → 200
- `POST https://auth.sheeplab.net/v1/auth/mfa/verify` → 200
- `GET  https://auth.sheeplab.net/v1/user/profile` → 200

つまり **AuthCore は生きているが、このリポジトリのバックエンドが `localhost:8082` で listen していない**。ネットワーク経路や CORS ではなく「プロセス自体が起動していない／落ちている」のほぼ確定。

### 主仮説（根本原因の最有力候補）

`docker-compose.yml` 16 行目:

```yaml
DB_PORT: 5433
```

これは backend コンテナから見た postgres への接続ポートだが、同ファイル 42-57 行目の postgres サービス定義を見ると:

- コンテナ内 listen ポートは **5432**（postgres イメージのデフォルト、`healthcheck` も `pg_isready` デフォルト）
- ホスト公開マッピングは `"${DB_PORT:-5433}:5432"` → ホスト 5433 → コンテナ 5432
- コンテナ間通信は `postgres:5432` で行う必要がある

backend コンテナから `postgres:5433` に接続しようとしても、postgres は 5433 で listen していないので **DB 接続失敗 → `config.Load` は通るが起動後の DB 初期化で exit、もしくは backend サービスがクラッシュループ** して、結果としてホスト側 `localhost:8082` にバインドされたプロセスが存在しなくなる。

補足観測:

- `Makefile:11` と `.env.example:11` と `config/config.go:72`（default 5432）と `db/init.sh:14`（default 5432）と `.github/workflows/ci.yml:93`（5432）は **すべて 5432** で一貫
- **唯一 `docker-compose.yml:16` だけが 5433** → ここが原因で間違いない
- `.env.example:5` の `SERVER_PORT=8082` とフロントの期待（`localhost:8082`）は一致、`docker-compose.yml:10` の `"${SERVER_PORT:-8080}:8080"` も `.env` で `SERVER_PORT=8082` が入っていれば `8082:8080` にマップされ整合（こちら側は問題なし）

### 副次的に確認すべき点（Phase 2 関連）

現ブランチ `feat/cookie-session-handoff-phase2` は `10-cookie-session-handoff.md` に沿って Cookie ハンドオフを実装中。`config/config.go` は `SESSION_COOKIE_*` 系の env を読み、`Validate` で以下の厳格チェックを行う:

- `SESSION_COOKIE_SAMESITE` が `Lax` / `Strict` / `None` 以外ならエラー
- `SESSION_COOKIE_SAMESITE=None` かつ `SESSION_COOKIE_SECURE=false` はエラー
- `SESSION_COOKIE_SECURE=false` かつ `ENVIRONMENT != development` はエラー
- `SESSION_COOKIE_FALLBACK_MAX_AGE <= 0` はエラー

`docker-compose.yml` の backend `environment:` にはまだ `SESSION_COOKIE_*` が渡されていないので、デフォルト（`Lax`, `Secure=true`, fallback `1h`）で起動はするはず。ただし **ローカル開発が http://localhost の場合、FE が `Secure` cookie を受信できず handoff できない** 問題は Phase 2 で別途拾う必要がある（このタスクのスコープ外だが、`.env.example` 差分の意図を確認する要因として記録）。

## 影響範囲

主に:

- `docker-compose.yml`（`DB_PORT: 5433` → `5432` 修正）
- `.env.example`（Phase 2 で追加した Cookie 関連と `SERVER_PORT` 変更の未コミット差分をレビュー確定）

二次的に（必要なら）:

- `docker-compose.yml` backend サービスの `environment:` に `SESSION_COOKIE_*` を追加（Phase 2 タスク側のスコープで対応するなら本タスクでは触らない）

破壊的変更: なし（ローカル開発環境の設定修正のみ）。

## 実装ステップ

### 1. 状態確認（事実収集）

ユーザーにまずこれを実行してもらう（もしくは agent が shell 可能なら自分で）:

```bash
docker compose ps
docker compose logs --tail=200 backend
docker compose logs --tail=50 postgres
ss -ltnp | grep 8082 || lsof -i :8082
cat .env 2>/dev/null | grep -E '^(SERVER_PORT|DB_PORT|DB_HOST|ENVIRONMENT)='
```

期待する判定:

- `backend` サービスが `Exit` / `Restarting` → 本仮説通り（DB 接続失敗でクラッシュ）
- `backend` ログに `dial tcp postgres:5433: connect: connection refused` 類 → 確定
- `ss` / `lsof` で 8082 に listen なし → 確定

### 2. `docker-compose.yml` の `DB_PORT` を修正

`docker-compose.yml:16`:

```yaml
# Before
DB_PORT: 5433
# After
DB_PORT: 5432
```

理由: コンテナ間通信では postgres コンテナ内部の listen ポート (`5432`) を使う。ホスト公開ポート (`5433`) はホストからの接続（`psql -h localhost -p 5433`）専用であり、backend コンテナからは関係ない。

### 3. 再起動して疎通確認

```bash
docker compose down
docker compose up -d --build backend
docker compose logs -f backend    # "server listening on :8080" 類のログを確認
curl -v http://localhost:8082/healthz   # 200 or 404 でも "接続できる" ことが重要
```

### 4. `.env.example` 差分のレビュー確定

現ブランチで `.env.example` が変更されている（未コミット）。`SERVER_PORT=8082` への変更や OGP / Cookie 関連の追記がローカル運用と整合するかを確認。Phase 2 タスク側で確定させる場合、ここでは「現状の差分をそのまま残すことが妥当か」を判断するだけに留める。

### 5. 原因と再発防止のメモを `docs/tasks/10-cookie-session-handoff.md` に追記

Phase 2 で `docker-compose.yml` に手を入れた際の副作用だった場合、関連タスクの「Known Pitfalls」節に一行追記して次回の踏み抜きを防ぐ。そうでなければスキップ。

### 6. PR 化

- ブランチ: `fix/docker-compose-db-port`（もしくは Phase 2 ブランチに混ぜずに develop から切る）
- PR base: `develop`（main ではない）
- 差分: `docker-compose.yml` 1 行のみが理想

## テスト要件

- `docker compose up -d` 後、`docker compose ps` で backend が `Up`（Exit/Restarting でない）
- `curl http://localhost:8082/...` が `ERR_CONNECTION_REFUSED` にならない（認証必要エンドポイントは 401 でよい、「接続自体は通る」ことが確認できればよい）
- フロントから `/timeline/home` `/me` `/v1/images` `/posts` を叩いて、401 or 200 相当のレスポンスが返る（接続拒否ではない）
- 既存の CI (unit test / lint) に影響がないことを確認

## 技術的な補足・制約

- **主仮説が外れた場合のフォールバック調査順序**:
  1. `.env` ファイルが存在しない / `DB_HOST` 等が空 → `config.Validate` がエラーで exit
  2. `SERVER_PORT=8082` を `.env` に設定しないまま `docker compose up` → ホスト側マッピングが `8080:8080` になり `localhost:8082` に何も listen しない（FE 側の期待ポートとズレている）
  3. Phase 2 で追加した `SESSION_COOKIE_*` 周りのバリデーションが `.env` 設定と衝突して `config.Load` が error 返して exit
  4. backend の Dockerfile ビルドが壊れている（`docker compose build backend` ログを確認）
  5. ホスト側で別プロセスが 8082 を占有していて bind 失敗（`lsof -i :8082`）
- **ポート命名の混乱を整理するメモ（修正と同時に README / DATABASE_SETUP に追記する価値あり）**:
  - `DB_PORT`（ホスト環境変数、`.env` / Makefile から使う）: ホスト側の公開ポート。`docker-compose.yml` 49 行目の左側にのみ使う
  - backend コンテナから見た postgres の接続ポート: **常に 5432**（postgres のコンテナ内 listen ポート）。ホスト側 `DB_PORT` とは別物として docker-compose 上で固定すべき
  - 今の `docker-compose.yml` はこの 2 つを混同している（16 行目に 5433 を書いたのは、49 行目のホスト側マッピング値と取り違えたミス）
- **`develop` ブランチ上で同じ問題が再現するかを確認**: もし `develop` では `5432` だったのに `feat/cookie-session-handoff-phase2` 作業中に誤って `5433` に書き換えた差分なら、その差分は revert すれば済む。逆に `develop` 時点から 5433 なら、そもそも誰もローカル docker で backend を起動していなかった可能性が高い（全員ローカル直接起動 `go run ./cmd/server` していた）。この判断によって、他の開発者への周知の要否が変わる。

## 調査で判明している事実（ドキュメント作成時点）

- `docker-compose.yml:16` → `DB_PORT: 5433`
- `docker-compose.yml:49` → `"${DB_PORT:-5433}:5432"`
- `docker-compose.yml:54` → postgres healthcheck: `pg_isready -U ${DB_USER:-fuju_user}`（ポート指定なし = 5432）
- `.env.example:11` → `DB_PORT=5432`
- `Makefile:11` → `DB_PORT ?= 5432`
- `config/config.go:72` → `getEnvInt("DB_PORT", 5432)`
- `db/init.sh:14` → `DB_PORT=${DB_PORT:-5432}`
- `.github/workflows/ci.yml:93` → `DB_PORT: 5432`

→ `docker-compose.yml:16` の `5433` のみが外れ値。ここが原因である蓋然性が極めて高い。
