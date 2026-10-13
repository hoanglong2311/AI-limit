# AI Limit

Ứng dụng menu bar cho macOS hiển thị hạn mức sử dụng của các dịch vụ AI
(Claude, Codex, Grok Build, DeepSeek...). Viết bằng Go với
[MyGo](https://github.com/egoist/mygo) v0.4.8, dùng UI native (không webview),
pure Go, không cần cgo.

## Yêu cầu

- macOS
- Go 1.27.2 trở lên (`brew install go` hoặc tải từ https://go.dev/dl/)

## Chạy

```sh
go run ./cmd/ai-limit
```

Biểu tượng xuất hiện trên menu bar; bấm vào để xem danh sách tài khoản. Phím Esc
để ẩn, Cmd+Q để thoát.

Tham số:

- `-interval 5m`: chu kỳ làm mới mặc định cho mỗi provider
- `-claude-email you@example.com`: email hiển thị cho tài khoản Claude
- `-demo=false`: ẩn các provider mock

## Build

```sh
go build -o build/ai-limit ./cmd/ai-limit
# Đóng gói .app (LSUIElement = true, không hiện trên Dock), theo mygo.json:
go run github.com/egoist/mygo/cmd/mygo@v0.4.8 build
```

Có thể cross-compile từ Linux: `GOOS=darwin GOARCH=arm64 go build ./cmd/ai-limit`.

## Kiểm thử

```sh
go test -race ./...
```

## Kiến trúc

- `internal/provider`: interface `Provider` và `Usage` (quota: % đã dùng +
  thời điểm reset; hoặc số dư kèm loại tiền).
- `internal/scheduler`: mỗi provider chạy trong goroutine riêng, có timeout
  và recover panic, nên một provider lỗi không ảnh hưởng các provider khác.
  Trạng thái mỗi dòng: `chưa cập nhật` (chưa lấy được hoặc dữ liệu đã cũ quá
  3 chu kỳ), `lỗi` (lần gọi gần nhất thất bại). Khi không mới, số liệu cũ
  không được hiển thị; chỉ hiện thời điểm cập nhật thành công gần nhất.
- Dòng dùng trên 80% được in đậm và tô nền.
- `internal/keychain`: đọc secret từ macOS Keychain (chỉ đọc, qua
  `/usr/bin/security`).
- `internal/providers`: `mock` (demo), `deepseek`, và các stub `claude`,
  `codex`, `grokbuild` (xem TODO trong code).

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
Lần đầu macOS có thể hỏi quyền cho `security` đọc mục này; chọn "Always Allow".

Claude: app chỉ đọc token mà Claude Code đã lưu (mục
`Claude Code-credentials`) và không bao giờ refresh token.

## Trạng thái

Đã có: mock, DeepSeek. Claude, Codex, Grok Build đang là stub và sẽ hiện `lỗi`.
