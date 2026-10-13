# AI Limit

Ứng dụng menu bar cho macOS hiển thị hạn mức sử dụng của các dịch vụ AI
(Claude, Codex, Grok Build, DeepSeek...). Viết bằng Go với
[MyGo](https://github.com/egoist/mygo) v0.4.8, dùng UI native (không webview),
pure Go, không cần cgo.

## Cài đặt (bản phát hành)

1. Tải `AI-limit-<phiên bản>-universal.dmg` ở trang Releases (chạy được trên
   cả Apple Silicon và Intel, macOS 12 trở lên).
2. Mở file dmg (nhấp đúp). Nếu Finder chưa tự mở cửa sổ, mở ổ `AI-limit`
   ở thanh bên Finder. Kéo `AI-limit.app` vào thư mục `Applications` (lối tắt
   có sẵn trong dmg), rồi tháo (eject) ổ `AI-limit`.
3. App chưa được ký bằng Developer ID nên lần đầu macOS sẽ chặn. Mở
   `AI-limit` một lần, bấm Done/OK, rồi vào System Settings > Privacy &
   Security, kéo xuống và bấm "Open Anyway" cho AI-limit, xác nhận bằng mật
   khẩu. Các lần sau mở bình thường.
4. Biểu tượng xuất hiện trên menu bar (không có trên Dock). Bấm vào để xem
   từng dịch vụ (Claude, Codex, DeepSeek) với thanh 5h và tuần, thời gian
   còn lại tới lúc reset và một dòng "Cập nhật lúc" ở cuối.
5. Kiểm tra file tải về (tuỳ chọn): `shasum -a 256 AI-limit-*.dmg`, so với
   SHA-256 trong ghi chú phát hành.

Nâng cấp: thoát app (Thoát hoặc Cmd+Q), kéo bản mới vào `Applications`
và chọn Replace.

## Cấu hình

Khi mở từ Finder không có tham số dòng lệnh, nên app đọc file
`~/Library/Application Support/AI-limit/config.json`. Nếu chưa có, app tạo
file với giá trị mặc định:

```json
{
  "claude_email": "",
  "codex_label": "ChatGPT",
  "interval": "5m",
  "demo": false
}
```

- `claude_email`: để trống thì app tự lấy email thật của tài khoản Claude
  (từ `~/.claude.json` của Claude Code hoặc API profile); không có email thì
  ẩn nhãn. Địa chỉ mẫu như `you@example.com` bị bỏ qua.
- `codex_label`: nhãn hiển thị cho tài khoản Codex.
- `interval`: chu kỳ làm mới (định dạng Go như `5m`, `10m`; tối thiểu `1m`;
  Claude luôn tối thiểu 5 phút).
- `demo`: `true` để hiện thêm các dòng mock.

Sửa file rồi thoát app (nút Thoát hoặc Cmd+Q) và mở lại. File này không chứa
secret. Tham số dòng lệnh (`-claude-email`, `-codex-label`, `-interval`,
`-demo`) vẫn ghi đè giá trị trong file.

## Phát triển

### Yêu cầu

- macOS
- Go 1.27.2 trở lên (`brew install go` hoặc tải từ https://go.dev/dl/)

### Chạy

```sh
go run ./cmd/ai-limit
```

Biểu tượng xuất hiện trên menu bar; bấm vào để xem danh sách tài khoản. Phím Esc
để ẩn, Cmd+Q để thoát.

Tham số:

- `-interval 5m`: chu kỳ làm mới mặc định cho mỗi provider
- `-claude-email you@example.com`: email hiển thị cho tài khoản Claude
- `-demo=true`: hiện các provider mock (mặc định tắt)

### Build

```sh
go build -o build/ai-limit ./cmd/ai-limit
# Đóng gói .app (LSUIElement = true, không hiện trên Dock), theo mygo.json:
go run github.com/egoist/mygo/cmd/mygo@v0.4.8 build
```

Đóng gói `.app` universal và `.dmg` UDZO kéo-thả (chạy được trên macOS hoặc Linux):

```sh
./scripts/package-macos.sh   # -> dist/AI-limit-<version>-universal.dmg
```

Trên Linux cần thêm `rcodesign`, `mkfs.hfsplus` (hfsprogs), `hfsplus` và `dmg`
từ libdmg-hfsplus, và Python có `ds_store` + `mac_alias` (đặt
`PYTHON=/đường/dẫn/python` nếu dùng venv). Chi tiết: `docs/dmg-plan.md`.
Ảnh nền cửa sổ dmg: `scripts/dmg/background.png` (tạo bằng
`go run ./scripts/dmg/background`).

Có thể cross-compile từ Linux: `GOOS=darwin GOARCH=arm64 go build ./cmd/ai-limit`.

### Kiểm thử

```sh
go test -race ./...
```

## Kiến trúc

- `internal/provider`: interface `Provider` và `Usage` (quota: % đã dùng +
  thời điểm reset; hoặc số dư kèm loại tiền).
- `internal/scheduler`: mỗi provider chạy trong goroutine riêng, có timeout
  và recover panic, nên một provider lỗi không ảnh hưởng các provider khác.
  Trạng thái mỗi dòng: `chưa cập nhật` (chưa lấy được, dữ liệu đã cũ quá
  3 chu kỳ, hoặc token thiếu/hết hạn), `lỗi` (lần gọi gần nhất thất bại). Khi không mới, số liệu cũ
  không được hiển thị; chỉ hiện thời điểm cập nhật thành công gần nhất.
- Dòng dùng trên 80% được in đậm và tô nền.
- `internal/keychain`: đọc secret từ macOS Keychain (chỉ đọc, qua
  `/usr/bin/security`).
- `internal/providers`: `mock` (demo), `deepseek`, `claude`, `codex`, và
  stub `grokbuild` (xem TODO trong code).

## Secret

Secret chỉ lưu trong Keychain, không bao giờ trong repo.

### DeepSeek

Lấy API key tại https://platform.deepseek.com/api_keys rồi thêm vào Keychain
(lệnh `-w` cuối cùng sẽ hỏi key, không để lộ key trong lịch sử shell):

```sh
security add-generic-password -s ai-limit.deepseek -a default -U -w
```

Kiểm tra: `security find-generic-password -s ai-limit.deepseek -w`.
Xoá: `security delete-generic-password -s ai-limit.deepseek`.

App gọi `GET https://api.deepseek.com/user/balance` và hiển thị số dư
(`total_balance`) theo USD nếu có; nếu tài khoản chỉ có CNY thì hiển thị
đúng CNY (không quy đổi). Thiếu key, lỗi HTTP hoặc JSON sai đều hiện `lỗi`.
Lần đầu macOS có thể hỏi quyền cho `security` đọc mục này; chọn "Allow" (chưa nên chọn "Always Allow" cho tới khi nghiệm thu xong).

### Claude

Cần đăng nhập Claude Code trên máy này. App đọc (chỉ đọc) credential OAuth mà Claude Code đã lưu
trong Keychain, mục `Claude Code-credentials` (JSON `claudeAiOauth` với
`accessToken`, `expiresAt`). App **không bao giờ** refresh hay ghi token;
nếu token thiếu, hết hạn hoặc bị từ chối (401), hai dòng Claude hiện
`chưa cập nhật` cho tới khi bạn mở Claude Code để nó tự làm mới.

Truyền email để đặt nhãn: `go run ./cmd/ai-limit -claude-email ban@example.com`.

App gọi `GET https://api.anthropic.com/api/oauth/usage` (header
`anthropic-beta: oauth-2025-04-20`), endpoint không công khai mà lệnh
`/usage` của Claude Code dùng, và hiện 2 dòng: `Claude 5h` và `Claude tuần`,
kèm thời điểm reset. Gọi tối đa 1 lần mỗi 5 phút vì endpoint này giới hạn
tần suất rất chặt (429 sẽ hiện `lỗi`). Endpoint có thể thay đổi bất cứ lúc nào.

### Codex

Không cần secret và không dùng mạng: app chỉ **đọc** log phiên mà Codex CLI
ghi ở `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` (hoặc `$CODEX_HOME`).
Mỗi lượt, Codex ghi một sự kiện `token_count` có `rate_limits.primary`
(cửa sổ 5 giờ, `window_minutes` 300) và `rate_limits.secondary` (tuần,
10080), với `used_percent` và `resets_at`. App lấy bản ghi mới nhất không
null trong 20 file mới nhất của các thư mục ngày hôm nay và 7 ngày trước
(theo giờ máy) và hiện 2 dòng `Codex 5h`, `Codex tuần`.
`~/.codex/auth.json` không bao giờ được mở.

Số liệu chỉ mới tới lượt Codex cuối cùng trên máy này. Dòng hiện
`chưa cập nhật` khi thời điểm reset đã qua, khi dữ liệu cũ hơn độ dài cửa
sổ, hoặc khi chưa có log có `rate_limits` (một số phiên bản Codex ghi `null`).
Nhãn tài khoản: `-codex-label ten-cua-ban`.

## Trạng thái

v0.1.1: giao diện theo khối dịch vụ, đếm ngược reset, màu thanh
xanh/vàng/đỏ, email Claude thật, DeepSeek báo "chưa có key", dmg có lối tắt Applications.
Cửa sổ cài đặt tự mở khi mount chưa được kiểm chứng trên Mac, dự kiến v0.1.2.
Grok Build dự kiến ở v0.2.0 (chưa hiện trong menu).
