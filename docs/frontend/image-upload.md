# 画像アップロード — フロントエンド実装ガイド

このドキュメントは FUJU バックエンドの画像アップロード API を **フロントエンドから呼ぶための実装指示書** です。バックエンドの内部設計（R2 / S3 互換 / Postgres）は知らなくても、本ドキュメントだけで実装できます。

## TL;DR

- エンドポイントは 3 つ: `POST /v1/images` / `GET /v1/images` / `DELETE /v1/images/{id}`
- 認証は AuthCore Bearer トークン（`Authorization: Bearer ...`）または httpOnly Cookie（`fuju_access`）
- `multipart/form-data` で `file` フィールドに画像を 1 枚乗せて POST
- 上限 5 MiB / `image/*` のみ
- 投稿に紐付けたい場合は、まず画像をアップロード → 返却された `id` を `POST /posts` の `image_ids` に渡す（2 ステップ）

## エンドポイント一覧

| Method | Path | 認証 | 用途 |
|---|---|---|---|
| `POST` | `/v1/images` | 必須 | 1 枚アップロード |
| `GET` | `/v1/images` | 必須 | 自分がアップロード済みの画像一覧 |
| `DELETE` | `/v1/images/{id}` | 必須 (所有者のみ) | 1 枚削除 |

> R2 がサーバー側で未設定の環境（一部の dev 環境）では 3 つとも 404 を返します。FE 側はこれを「機能が無効化されている」シグナルとして扱ってください。

## 1. アップロード — `POST /v1/images`

### リクエスト

- `Content-Type: multipart/form-data`
- フィールド `file`: 画像バイナリ（必須、1 枚のみ）

### 制約

- 最大ファイルサイズ: **5 MiB**（5 * 1024 * 1024 bytes）
- MIME タイプ: **`image/*` のみ**（`image/jpeg`, `image/png`, `image/gif`, `image/webp` 等）
- サーバー側で「アップロード時の Content-Type」と「先頭バイトの sniff 結果」を二重チェックします。HTML を `image/jpeg` と詐称すると 400 で弾かれます。

### レスポンス（201 Created）

```json
{
  "data": {
    "id": "01HXYZ...",
    "file_name": "cat.jpg",
    "mime_type": "image/jpeg",
    "file_size": 123456,
    "public_url": "https://images.fuju.example.com/images/01HUSER.../01HOBJ.../cat.jpg",
    "user_id": "01HUSER...",
    "created_at": "2026-05-09T12:34:56Z",
    "updated_at": "2026-05-09T12:34:56Z"
  }
}
```

- `id`: 画像の ULID。投稿に紐付けるとき (`POST /posts` の `image_ids`) で使う。
- `public_url`: そのまま `<img src=...>` に渡せる URL。ブラウザから直接 GET 可能。
- `storage_key` などの内部キーは **返却されません**。FE で URL を組み立てる必要はなし。

### エラー

| HTTP | 条件 | UI 上のおすすめ表示 |
|---|---|---|
| 400 | `file` フィールド無し | 「ファイルを選択してください」 |
| 400 | サイズ > 5 MiB | 「5 MB 以下の画像を選んでください」 |
| 400 | `image/*` 以外 / sniff 不一致 | 「画像ファイルを選んでください」 |
| 401 | 未認証 / トークン期限切れ | ログイン画面へリダイレクト |
| 404 | サーバーで R2 未設定 | 機能フラグで画像 UI を非表示化 |
| 500 | R2 / DB の障害 | 「アップロードに失敗しました。時間をおいてお試しください」 |

### 実装例（fetch）

```ts
async function uploadImage(file: File, accessToken: string): Promise<UploadedImage> {
  if (file.size > 5 * 1024 * 1024) {
    throw new Error("5MB を超える画像はアップロードできません");
  }
  if (!file.type.startsWith("image/")) {
    throw new Error("画像ファイルを選んでください");
  }

  const formData = new FormData();
  formData.append("file", file);

  const res = await fetch("/v1/images", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${accessToken}`,
      // Content-Type は **指定しない**。fetch が boundary 付きで自動設定する。
    },
    body: formData,
  });

  if (res.status === 401) {
    // ログイン UI へ
    throw new Unauthorized();
  }
  if (res.status === 404) {
    throw new Error("画像アップロード機能が利用できません");
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err?.error?.message ?? "アップロードに失敗しました");
  }

  const json = await res.json();
  return json.data;
}
```

### 実装例（axios）

```ts
import axios from "axios";

const formData = new FormData();
formData.append("file", file);

const { data } = await axios.post("/v1/images", formData, {
  headers: { Authorization: `Bearer ${token}` },
  // Content-Type は axios 側で multipart/form-data + boundary を自動付与
});

return data.data; // { id, public_url, ... }
```

### 進捗表示（任意）

`fetch` には進捗イベントが無いので、進捗バーが要るなら `XMLHttpRequest` の `upload.onprogress`、または axios の `onUploadProgress` を使う。

```ts
await axios.post("/v1/images", formData, {
  headers: { Authorization: `Bearer ${token}` },
  onUploadProgress: (e) => {
    if (e.total) setProgress(Math.round((e.loaded * 100) / e.total));
  },
});
```

## 2. 一覧取得 — `GET /v1/images`

自分がアップロードした画像（soft delete されていないもの）を **作成日時降順** で全件返します。ページネーションは現状ありません（`limit=100, offset=0, total=件数` 固定）。件数が増えてページングが必要になったらバックエンドへ別途相談してください。

### レスポンス（200 OK）

```json
{
  "data": [
    { "id": "01H...", "public_url": "https://...", "file_name": "...", ... },
    ...
  ],
  "limit": 100,
  "offset": 0,
  "total": 12
}
```

## 3. 削除 — `DELETE /v1/images/{id}`

- 所有者本人のみ削除可（他人の画像は 403）
- ソフトデリート: DB 上の `deleted_at` が立つ。R2 オブジェクト削除はベストエフォート（失敗しても 204 を返す）
- レスポンスは **204 No Content**（ボディなし）

### エラー

| HTTP | 条件 |
|---|---|
| 400 | `id` が ULID 形式ではない |
| 401 | 未認証 |
| 403 | 自分の画像ではない |
| 404 | 画像が存在しない（または既に削除済み） |

## 4. 投稿に画像を紐付ける（典型フロー）

1 枚または複数の画像を投稿に貼るには、**先に画像をアップロード → 取得した `id` を投稿作成時に渡す** という 2 ステップを踏みます。

```ts
// Step 1: 画像を順にアップロード（並列でも可）
const uploaded = await Promise.all(files.map(f => uploadImage(f, token)));
const imageIds = uploaded.map(u => u.id);

// Step 2: 投稿作成
await fetch("/posts", {
  method: "POST",
  headers: {
    Authorization: `Bearer ${token}`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    content: "今日のごはん",
    image_ids: imageIds, // ← ここに画像 ID を配列で
  }),
});
```

`image_ids` の **配列順** が投稿表示時の画像並び順になります（`post_images.position`）。

## 5. 注意事項

- **画像変換は行いません**: バックエンドはアップロードされたバイナリをそのまま R2 に保存します。リサイズ・WebP 変換・EXIF 除去等が必要なら **FE 側でやってから** アップロードしてください（プライバシー上の理由で EXIF を落としたいケースは特に）。
- **Content-Type を手動指定しない**: `multipart/form-data` 送信時、`Content-Type` ヘッダは fetch / axios に任せる。手で `multipart/form-data` を書くと boundary がずれて 400 になります。
- **public_url は CDN 経由になる場合がある**: ドメインは `R2_PUBLIC_DOMAIN` 環境変数で運用側が設定するもので、`api.fuju.example.com` とは別オリジンになる可能性があります。`<img>` タグへの埋め込みは問題なし。canvas で読み出すなど CORS が要るケースは個別に R2 側 CORS を相談してください。
- **アップロード後の画像は誰でも見られる**: 公開バケット運用前提なので、`public_url` を知っている人は誰でも GET できます。プライベート画像が必要な要件が出たら別途設計が必要です。

## 6. swagger.yaml との対応

OpenAPI スキーマは `docs/swagger.yaml` の `/v1/images`（349-449 行目あたり）と `Image` / `ImageEnvelope` / `ImageListResponse`（1199-1235, 1340-1346, 1404-1420 行目あたり）を参照。型生成は `openapi-typescript` または `orval` でこの YAML から行う想定です。
