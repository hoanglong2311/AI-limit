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
  Trạng thái mỗi dòng: `chưa cập nhật` (chưa lấy được, dữ liệu đã cũ quá
  3 chu kỳ, hoặc token thiếu/hết hạn), `lỗi` (lần gọi gần nhất thất bại). Khi không mới, số liệu cũ
  không được hiển thị; chỉ hiện thời điểm cập nhật thành công gần nhất.
- Dòng dùng trên 80% được in đậm và tô nền.
- `internal/keychain`: đọc secret từ macOS Keychain (chỉ đọc, qua
  `/usr/bin/security`).
- `internal/providers`: `mock` (demo), `deepseek`, `claude`, và các stub
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
Lần đầu macOS có thể hỏi quyền cho `security` đọc mục này; chọn "Allow" (chưa nên chọn "Always Allow" cho tới khi nghiệm thu xong).

### Claude

Không cần thêm gì: app đọc (chỉ đọc) credential OAuth mà Claude Code đã lưu
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

## Trạng thái

Đã có: mock, DeepSeek, Claude. Codex, Grok Build đang là stub và sẽ hiện `lỗi`.
