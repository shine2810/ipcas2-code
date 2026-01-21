# IPC-Toyz

Công cụ quản lý và cập nhật IPCAS2 cho Agribank.

## Tính năng

- ⬇️ **Update** - Cập nhật IPCAS2 từ server với MD5 verification
- 🔍 **Quét** - Tìm & xóa file IPCAS2.ini ẩn
- ⏰ **Timer** - Hẹn giờ tắt máy
- 💾 **Ổ đĩa** - Map ổ mạng & dọn file rác (.env, .enk, ảnh cũ)
- 📊 **Info** - MAC/IP/Hostname/Ping + Đổi tên máy + Join Domain
- ⚙️ **INI** - Cấu hình IPCAS2.ini
- 🌐 **Region** - Định dạng ngày/số
- 👤 **About** - Thông tin & hướng dẫn

## Build từ source

### Yêu cầu
- Go 1.21+
- GCC (MSYS2 MinGW-w64 hoặc TDM-GCC)

### Build local
```bash
go build -o IPC-Toyz.exe .
```

### Build qua GitHub Actions (Khuyến nghị)
1. Push code lên GitHub
2. Actions tự động build
3. Download exe từ Artifacts

## Tác giả

**Phan Tiến**  
Agribank Chi nhánh Tây Nghệ An  
📞 0945626999

## License

Proprietary - All rights reserved
