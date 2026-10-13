# dmg kéo-thả (đã làm ở v0.1.1)

`scripts/package-macos.sh` làm đúng các bước dưới. Ghi chú thực tế:
libdmg-hfsplus dùng bản planetbeing (bản fanquake đã bỏ công cụ `hfsplus`),
tắt OpenSSL (FileVault) khi build vì không tương thích OpenSSL 3; bước 5
là `dmg build` (ảnh HFS+ thô), không phải `dmg dmg`. Debian 13 không còn
gói hfsprogs: lấy `hfsprogs_540.1.linux3-6_amd64.deb` từ snapshot.debian.org.

Hướng làm trên Linux (không cần Mac):

1. Tạo ảnh HFS+ thô: `dd` + `mkfs.hfsplus -v "AI-limit 0.1.1"` (gói `hfsprogs`).
2. Chép nội dung bằng `hfsplus` (build từ libdmg-hfsplus, fork
   fanquake/mozilla): `add AI-limit.app`, `symlink Applications /Applications`,
   `mkdir .background` + `add background.png`, `add .DS_Store`, `chmod` giữ exec bit.
3. `.DS_Store` sinh bằng Python `ds_store` + `mac_alias` (như dmgbuild):
   kích thước cửa sổ, ẩn toolbar, icon size, vị trí AI-limit.app (trái) và
   Applications (phải), `backgroundImageAlias` trỏ tới `.background/background.png`
   (alias cần đúng tên volume và CNID → sinh sau khi đã add file, đọc CNID bằng `hfsplus ls`).
4. Tự mở cửa sổ khi mount: macOS dùng `finderInfo[0]` (= CNID thư mục gốc, 2)
   trong volume header (`bless --openfolder`). libdmg-hfsplus không làm việc này →
   vá 4 byte ở offset 1024+80 của ảnh HFS+ bằng một script Go nhỏ trước bước 5.
5. `dmg build raw.img AI-limit-0.1.1-universal.dmg` → UDZO (zlib) + koly trailer.
6. Kiểm tra trên Mac: `hdiutil verify`, mở dmg, cửa sổ tự hiện đúng bố cục.

Không publish release.
