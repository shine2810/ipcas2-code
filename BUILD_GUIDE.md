# Hướng dẫn Build & Deploy IPC-Toyz

## Phương pháp: GitHub Actions (Khuyến nghị cho sản phẩm bán)

### Tại sao dùng GitHub Actions?
- ✅ Build trên **Windows Server chính thức** của Microsoft
- ✅ Exe **100% tương thích** với mọi máy Windows
- ✅ **Miễn phí** cho public repository
- ✅ **Tự động** - chỉ cần push code

### Bước 1: Tạo GitHub Repository

1. Truy cập: https://github.com/new
2. Tạo repository mới (có thể để Private nếu muốn)
3. Đặt tên: `ipcas2-scanner` (hoặc tên bạn thích)
4. **KHÔNG** tick "Initialize with README"
5. Click "Create repository"

### Bước 2: Upload Code

**Cách 1: Sử dụng GitHub Desktop (Đơn giản nhất)**
1. Download: https://desktop.github.com/
2. Install và login
3. File → Add Local Repository → Chọn `d:\Golang\ipcas2-scanner`
4. Publish repository

**Cách 2: Upload qua Web (Nếu không muốn cài Git)**
1. Trên GitHub repository page, click "uploading an existing file"
2. Kéo thả **TẤT CẢ** files từ `d:\Golang\ipcas2-scanner` (trừ các file .exe, .dll)
3. Files quan trọng cần upload:
   - `main.go`
   - `go.mod`
   - `.github/workflows/build.yml` (rất quan trọng!)
   - `icon.png`
   - `README.md`
   - `.gitignore`
   - Folder `fonts/`, `static/`, `scanner/`

### Bước 3: Chạy GitHub Actions

1. Sau khi upload xong, vào tab **Actions** trên GitHub
2. Click vào workflow "Build IPC-Toyz"
3. Click nút "Run workflow" → "Run workflow"
4. Đợi ~2-5 phút để build

### Bước 4: Download Exe

1. Sau khi build xong (màu xanh ✅)
2. Click vào build run
3. Kéo xuống phần **Artifacts**
4. Download `IPC-Toyz-Windows.zip`
5. Giải nén → **IPC-Toyz.exe** sẽ chạy OK!

---

## Phương pháp khác: Build trên máy khác

Nếu không muốn dùng GitHub, có thể:
1. Mang source code sang máy khác có Visual Studio
2. Hoặc cài Visual Studio Build Tools trên máy này
3. Build lại sẽ OK

---

## Support

Nếu gặp vấn đề khi setup GitHub Actions, liên hệ:
- Phan Tiến: 0945626999
