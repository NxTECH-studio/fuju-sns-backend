# FUJU Backend タスク計画インデックス

このディレクトリは、`/start-with-plan` から実装を駆動するためのタスク計画ドキュメントを集めたものです。

## 番号付けルール

各タスクファイル名の先頭には `NN-` 形式のプレフィックスが付いています。意味は以下の通りです:

- **番号が小さいほど前段（=依存先）**。先に完了している必要があります。
- **同じ番号同士は並行実装が可能**。依存関係がないので好きな順に着手できます。
- 番号は「実装順序の強制」ではなく「依存関係のレイヤ番号」を表します。
- リネーム・削除・追加があれば、この README と各ドキュメント冒頭の「前提（依存タスク）」セクションを同時に更新してください。

## タスク一覧（依存グラフ）

```
            01-rewrite-domain-to-authcore-alignment
             |
             +---- 02-implement-post-feature ----+---- 03-implement-follow-and-timeline
             |                                    |
             |                                    +---- 03-implement-ogp-fetcher ---- 05-fix-ogp-attach-position
             |
             +---- 02-implement-badge

            04-fix-ci-lint-and-pragent  (独立: CI lint 衛生)
             |
            05-fix-ogp-attach-position  (03-ogp follow-up)
             |
            06-frontend-handoff-docs-and-dead-code-removal  (01-05 完了前提)
             |
            07-fix-ci-go-version-and-lint-hygiene  (04/05/06 から独立に並行可だが、CI を緑に戻す緊急性から先に捌く)
             |
            08-wire-postgres-repository  (01-07 完了前提、production DB 配線)
```

## タスク一覧（表）

| # | ファイル | 依存 | ステータス |
|---|---|---|---|
| 01 | [01-rewrite-domain-to-authcore-alignment.md](./01-rewrite-domain-to-authcore-alignment.md) | なし | 確定済み。`users.is_admin` カラム追加、`images.id` の UUID → ULID(CHAR(26)) 変換、001 の破壊的書き換えを含む |
| 02 | [02-implement-post-feature.md](./02-implement-post-feature.md) | 01 | 確定済み。本文上限 **120 文字** に確定 |
| 02 | [02-implement-badge.md](./02-implement-badge.md) | 01 | 確定済み。青=`verified_celebrity`, 金=`developer`（priority=5）, admin 判定は `users.is_admin`, 初回 admin は seed migration（Migration 005）で投入 |
| 03 | [03-implement-follow-and-timeline.md](./03-implement-follow-and-timeline.md) | 02-post | 確定済み。`users.followers_count` / `following_count` は本タスクの Migration 006 で ALTER |
| 03 | [03-implement-ogp-fetcher.md](./03-implement-ogp-fetcher.md) | 02-post | 確定済み。キャッシュ TTL=3 日、末尾スラッシュは同一視、enqueue は post commit 後 best-effort |
| 04 | [04-fix-ci-lint-and-pragent.md](./04-fix-ci-lint-and-pragent.md) | 03-ogp | 確定済み。`golangci-lint v2.11.4` 違反解消 + PRAgent 再実装 |
| 05 | [05-fix-ogp-attach-position.md](./05-fix-ogp-attach-position.md) | 03-ogp | 確定済み。`ogp_jobs.position` 追加（Migration 008）+ OGPJobQueue.Enqueue シグネチャ拡張 |
| 06 | [06-frontend-handoff-docs-and-dead-code-removal.md](./06-frontend-handoff-docs-and-dead-code-removal.md) | 01-05 | 確定済み。dead code 削除 → swagger 刷新 → FE 向けプロダクト概要 / AuthCore 連携ドキュメント整備。migration 追加なし |
| 07 | [07-fix-ci-go-version-and-lint-hygiene.md](./07-fix-ci-go-version-and-lint-hygiene.md) | 04-06（CI を直す緊急タスクとして独立にも進む） | 確定済み。`.github/workflows/ci.yml` の `GO_VERSION` を `1.25` に bump + `cmd/server/main.go` exported var doc comment 追加。**現在 CI red のブロッカー、最優先** |
| 08 | [08-wire-postgres-repository.md](./08-wire-postgres-repository.md) | 01-07 | `[未確定]` あり（ORM / default backend / inmemory 残置）。`pkg/db` 新設 + `internal/repository/postgres/` に全 interface 実装 + CI に integration test 追加。4 Phase ≒ 4 PR に分割 |

> `04-implement-organization.md` は **廃案・削除済み**（MVP 非対応）。他ドキュメントからも参照を削除済み。

## マイグレーション番号の割り当て

既存は `db/migrations/` 配下に 3 桁連番（`001_initial_schema.sql`, `002_add_images_table.sql`）。以降も **3 桁連番** で続ける。

| 番号 | パス | タスク | 備考 |
|---|---|---|---|
| 001 | `db/migrations/001_initial_schema.sql` | （既存） | **01 タスクで破壊的書き換え**。旧 `sessions` / `oauth_states` / 旧 `likes` を削除し、新 `users` スキーマ（`sub CHAR(26)` PK, `is_admin` カラム含む）と `posts` / `comments`(後で廃止) の ULID 化後スキーマに揃える |
| 002 | `db/migrations/002_add_images_table.sql` | （既存） | **01 タスクで破壊的書き換え**。`images.id` を UUID → CHAR(26)(ULID)、`images.user_id` を BIGINT → CHAR(26) に変更 |
| 003 | `db/migrations/003_rewrite_users_for_authcore.sql` | 01 | 001/002 で直接書き換えて足りない追補があればここ。無ければスキップして 004 から連番で続けてよい（agent 判断。方針を 01 ドキュメント冒頭に明記） |
| 004 | `db/migrations/004_implement_posts.sql` | 02-post | posts の拡張、`post_images`, `likes`（CHAR(26) 版）, `tags`, `post_tags`、`comments` テーブル DROP |
| 005 | `db/migrations/005_implement_badges.sql` | 02-badge | `badges` / `user_badges` + seed（developer, verified_celebrity）+ 初回 admin への `UPDATE users SET is_admin=true` を同梱 |
| 006 | `db/migrations/006_implement_follow_and_timeline.sql` | 03-follow | `follows` + `users.followers_count` / `users.following_count` の ALTER |
| 007 | `db/migrations/007_implement_ogp_cache.sql` | 03-ogp | `ogp_cache` / `post_ogp` / `ogp_jobs` |
| 008 | `db/migrations/008_add_ogp_job_position.sql` | 05 | `ogp_jobs.position SMALLINT NOT NULL DEFAULT 0` 追加（**実装済み**） |
| 009〜 | （未定） | 08 以降で必要になれば | 08 タスク自体はスキーマ変更を伴わない想定（既存 001-008 の Go 側配線のみ）。本番運用中にスキーマ追加が必要になれば 009 から連番で割り当てる |

## `[未確定]` マーカーについて

各ドキュメント内で `[未確定]` と書かれている項目は、実装着手前にユーザー確認が必要な箇所です。実装中に迷ったら、まずドキュメントの当該箇所を再確認してください。

**2026-04-21 時点**:
- 01〜07 の `[未確定]` は解消済み（07 は Makefile/README への 1 行メモ追加要否のみ optional で残置）。
- 08 は `[未確定]` を 5 項目保持中: ①クエリ記述手段 ②default backing store ③inmemory 残置可否 ④migration CI step ⑤`GitCommit` var 追加是非。Phase 0 で確定させてから Phase 1 着手する。

## 並行着手時の注意

- `02-` 同士（`02-implement-post-feature.md` と `02-implement-badge.md`）は別の PR で並行できます。両者とも `01-` 完了を前提にしていて、互いに依存しません。
- `03-` 同士（`03-implement-follow-and-timeline.md` と `03-implement-ogp-fetcher.md`）は Post 完了後に並行できます。
- `07` は CI 緊急対処のため、`04`〜`06` と並行で進めて構いません（`ci.yml` の `GO_VERSION` と `cmd/server/main.go` の doc comment という触る範囲が狭いファイルだけなのでコンフリクトしません）。むしろ CI red 状態では他 PR の merge が進まないので **最優先で捌く** 想定。
- `08` は `07` で CI が緑に戻った後に着手するのが安全。Phase 1-4 の 4 PR に分割するので、レビューの並列度に合わせて進める。
- マイグレーション番号は衝突しないようタスク間で調整してください（新しいものが後に来る前提）。
