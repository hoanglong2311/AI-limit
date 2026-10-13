# Kế hoạch dmg kéo-thả (v0.1.1, làm SAU khi duyệt UI)

Hiện tại `scripts/package-macos.sh` trên Linux tạo ISO9660/HFS hybrid
(genisoimage): mount được nhưng không có cửa sổ kéo-thả, không nén UDZO.

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
5. `dmg dmg raw.img AI-limit-0.1.1-universal.dmg` → UDZO (zlib) + koly trailer.
6. Kiểm tra trên Mac: `hdiutil verify`, mở dmg, cửa sổ tự hiện đúng bố cục.

Không publish release.
