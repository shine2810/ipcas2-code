package main

import (
	"archive/zip"
	"crypto/md5"
	"embed"
	"fmt"
	"image/color"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed fonts/segoeui.ttf
var fontData embed.FS

const FILE_ATTRIBUTE_HIDDEN = 0x02

// Windows 11 Fluent Theme
type FluentTheme struct {
	font fyne.Resource
}

func NewFluentTheme() *FluentTheme {
	data, _ := fontData.ReadFile("fonts/segoeui.ttf")
	return &FluentTheme{
		font: &fyne.StaticResource{StaticName: "segoeui.ttf", StaticContent: data},
	}
}

func (t *FluentTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	// Windows 11 Fluent Design colors
	colors := map[fyne.ThemeColorName]color.Color{
		theme.ColorNameBackground:      color.NRGBA{R: 243, G: 243, B: 243, A: 255}, // Mica-like
		theme.ColorNameButton:          color.NRGBA{R: 251, G: 251, B: 251, A: 255}, // Subtle button
		theme.ColorNamePrimary:         color.NRGBA{R: 0, G: 103, B: 192, A: 255},   // Windows blue
		theme.ColorNameForeground:      color.NRGBA{R: 32, G: 32, B: 32, A: 255},    // Near black
		theme.ColorNameInputBackground: color.White,
		theme.ColorNameSeparator:       color.NRGBA{R: 229, G: 229, B: 229, A: 255},
		theme.ColorNameHover:           color.NRGBA{R: 245, G: 245, B: 245, A: 255},
		theme.ColorNameDisabled:        color.NRGBA{R: 160, G: 160, B: 160, A: 255},
		theme.ColorNamePlaceHolder:     color.NRGBA{R: 140, G: 140, B: 140, A: 255},
		theme.ColorNameScrollBar:       color.NRGBA{R: 200, G: 200, B: 200, A: 255},
	}
	if c, ok := colors[n]; ok {
		return c
	}
	return theme.DefaultTheme().Color(n, theme.VariantLight)
}

func (t *FluentTheme) Font(s fyne.TextStyle) fyne.Resource {
	return t.font
}

func (t *FluentTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(n)
}

func (t *FluentTheme) Size(n fyne.ThemeSizeName) float32 {
	sizes := map[fyne.ThemeSizeName]float32{
		theme.SizeNameText:           13,
		theme.SizeNameHeadingText:    18,
		theme.SizeNameSubHeadingText: 14,
		theme.SizeNamePadding:        8,
		theme.SizeNameInnerPadding:   6,
		theme.SizeNameScrollBar:      8,
	}
	if s, ok := sizes[n]; ok {
		return s
	}
	return theme.DefaultTheme().Size(n)
}

type FileInfo struct {
	Path string
	Size int64
}
type FileItem struct {
	info     FileInfo
	selected bool
}

var (
	win         fyne.Window
	files       []*FileItem
	fileList    *widget.List
	statusLbl   *widget.Label
	countLbl    *widget.Label
	sdLabel     *widget.Label
	scanning    bool
	stopChan    chan struct{}
	mutex       sync.Mutex
	hasShutdown bool
)

// Custom styled message dialog
func showMsg(title, message string) {
	// Choose icon and color based on title
	icon := "ℹ️"
	headerColor := color.NRGBA{R: 0, G: 103, B: 192, A: 255} // Blue
	if strings.Contains(strings.ToLower(title), "lỗi") || strings.Contains(strings.ToLower(title), "error") {
		icon = "❌"
		headerColor = color.NRGBA{R: 200, G: 50, B: 50, A: 255} // Red
	} else if strings.Contains(strings.ToLower(title), "thành công") || strings.Contains(strings.ToLower(title), "hoàn tất") {
		icon = "✅"
		headerColor = color.NRGBA{R: 0, G: 150, B: 80, A: 255} // Green
	} else if strings.Contains(strings.ToLower(title), "cảnh báo") {
		icon = "⚠️"
		headerColor = color.NRGBA{R: 220, G: 160, B: 0, A: 255} // Yellow
	}

	// Header bar
	headerBg := canvas.NewRectangle(headerColor)
	headerBg.SetMinSize(fyne.NewSize(260, 40))
	headerText := canvas.NewText(icon+" "+title, color.White)
	headerText.TextSize = 14
	headerText.Alignment = fyne.TextAlignCenter
	header := container.NewStack(headerBg, container.NewCenter(headerText))

	// Message
	msgText := canvas.NewText(message, color.NRGBA{R: 50, G: 50, B: 50, A: 255})
	msgText.TextSize = 13
	msgText.Alignment = fyne.TextAlignCenter

	var d dialog.Dialog

	okBtn := widget.NewButton("  OK  ", func() {
		d.Hide()
	})

	body := container.NewVBox(
		widget.NewLabel(""),
		container.NewCenter(msgText),
		widget.NewLabel(""),
		container.NewCenter(okBtn),
	)

	content := container.NewBorder(header, nil, nil, nil, container.NewPadded(body))

	d = dialog.NewCustomWithoutButtons(title, content, win)
	d.Show()
}

// Custom styled confirm dialog with proper text wrapping
func showConfirm(title, message string, onConfirm func()) {
	// Header with blue background
	headerBg := canvas.NewRectangle(color.NRGBA{R: 0, G: 103, B: 192, A: 255})
	headerBg.SetMinSize(fyne.NewSize(300, 40))
	headerText := canvas.NewText("⚠️ "+title, color.White)
	headerText.TextSize = 14
	headerText.Alignment = fyne.TextAlignCenter
	header := container.NewStack(headerBg, container.NewCenter(headerText))

	// Message with proper wrapping
	msgLabel := widget.NewLabel(message)
	msgLabel.Wrapping = fyne.TextWrapWord
	msgLabel.Alignment = fyne.TextAlignCenter

	var d *widget.PopUp

	yesBtn := widget.NewButton("✅ Đồng ý", func() {
		d.Hide()
		onConfirm()
	})
	noBtn := widget.NewButton("❌ Hủy", func() {
		d.Hide()
	})

	body := container.NewVBox(
		widget.NewLabel(""),
		msgLabel,
		widget.NewLabel(""),
		container.NewGridWithColumns(2, yesBtn, noBtn),
		widget.NewLabel(""),
	)

	bg := canvas.NewRectangle(color.White)
	bg.CornerRadius = 10

	content := container.NewBorder(header, nil, nil, nil, container.NewPadded(body))
	card := container.NewStack(bg, content)

	d = widget.NewPopUp(card, win.Canvas())
	d.Show()
}

// Update confirm dialog with 3 options
func showUpdateConfirm(title, message string, onBackupUpdate, onUpdateOnly func()) {
	titleText := canvas.NewText(title, color.NRGBA{R: 0, G: 103, B: 192, A: 255})
	titleText.TextSize = 14
	titleText.Alignment = fyne.TextAlignCenter

	msgText := widget.NewLabel(message)
	msgText.Wrapping = fyne.TextWrapWord
	msgText.Alignment = fyne.TextAlignCenter

	var d *widget.PopUp

	backupBtn := widget.NewButton("Backup rồi Update", func() {
		d.Hide()
		onBackupUpdate()
	})
	updateBtn := widget.NewButton("Update luôn", func() {
		d.Hide()
		onUpdateOnly()
	})
	cancelBtn := widget.NewButton("Hủy", func() {
		d.Hide()
	})

	content := container.NewVBox(
		widget.NewLabel(""),
		container.NewCenter(titleText),
		widget.NewLabel(""),
		msgText,
		widget.NewLabel(""),
		container.NewGridWithColumns(3, backupBtn, updateBtn, cancelBtn),
		widget.NewLabel(""),
	)

	bg := canvas.NewRectangle(color.White)
	bg.CornerRadius = 8

	card := container.NewStack(bg, container.NewPadded(content))

	d = widget.NewPopUp(card, win.Canvas())
	d.Show()
}

func isHidden(path string) bool {
	p, _ := syscall.UTF16PtrFromString(path)
	a, _ := syscall.GetFileAttributes(p)
	return a&FILE_ATTRIBUTE_HIDDEN != 0
}

func fmtSize(s int64) string {
	if s >= 1048576 {
		return fmt.Sprintf("%.1f MB", float64(s)/1048576)
	}
	if s >= 1024 {
		return fmt.Sprintf("%.1f KB", float64(s)/1024)
	}
	return fmt.Sprintf("%d B", s)
}

func scanFiles(stop <-chan struct{}) []FileInfo {
	var r []FileInfo
	for _, p := range []string{
		filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "VirtualStore", "Windows"),
		filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "VirtualStore"),
	} {
		if _, e := os.Stat(p); e != nil {
			continue
		}
		filepath.Walk(p, func(path string, info os.FileInfo, e error) error {
			select {
			case <-stop:
				return filepath.SkipAll
			default:
			}
			if e != nil || info.IsDir() {
				return nil
			}
			if strings.ToLower(info.Name()) == "ipcas2.ini" && isHidden(path) {
				r = append(r, FileInfo{path, info.Size()})
			}
			return nil
		})
	}
	return r
}

func delFile(path string) error {
	p, _ := syscall.UTF16PtrFromString(path)
	a, _ := syscall.GetFileAttributes(p)
	if a&FILE_ATTRIBUTE_HIDDEN != 0 {
		syscall.SetFileAttributes(p, a&^FILE_ATTRIBUTE_HIDDEN)
	}
	return os.Remove(path)
}

func main() {
	os.Setenv("FYNE_SCALE", "1")

	a := app.New()
	a.Settings().SetTheme(NewFluentTheme())

	if d, _ := os.ReadFile("icon.png"); len(d) > 0 {
		a.SetIcon(&fyne.StaticResource{StaticName: "icon", StaticContent: d})
	}

	win = a.NewWindow("IPC-Toyz")
	win.Resize(fyne.NewSize(400, 500))
	win.CenterOnScreen()
	win.SetContent(buildUI())

	go func() {
		time.Sleep(500 * time.Millisecond)
		if exec.Command("shutdown", "/a").Run() == nil {
			hasShutdown = true
			dialog.ShowInformation("Cảnh báo", "Phát hiện hẹn giờ tắt máy.\nĐã tự động hủy.", win)
		}
	}()

	win.ShowAndRun()
}

func buildUI() fyne.CanvasObject {
	// Header
	hdr := canvas.NewRectangle(color.NRGBA{R: 0, G: 103, B: 192, A: 255})
	hdr.SetMinSize(fyne.NewSize(0, 42))
	title := canvas.NewText("IPC-Toyz", color.White)
	title.TextSize = 16
	header := container.NewStack(hdr, container.NewCenter(title))

	// Footer with background
	ftrBg := canvas.NewRectangle(color.NRGBA{R: 240, G: 240, B: 240, A: 255})
	ftrBg.SetMinSize(fyne.NewSize(0, 30))
	ftrText := canvas.NewText("Phan Tiến - Agribank Tây Nghệ An", color.NRGBA{R: 80, G: 80, B: 80, A: 255})
	ftrText.TextSize = 10
	ftrText.Alignment = fyne.TextAlignCenter
	footer := container.NewStack(ftrBg, container.NewCenter(ftrText))

	// Create tab contents - Update first!
	updateContent := tabUpdate()
	scanContent := tabScan()
	shutdownContent := tabShutdown()
	networkContent := tabNetwork()
	sysInfoContent := tabSystemInfo()
	configContent := tabConfig()
	regionContent := tabRegion()
	authorContent := tabAuthor()

	// Content container - shows Update tab first
	contentStack := container.NewStack(updateContent)

	// Tab button style
	activeColor := color.NRGBA{R: 0, G: 103, B: 192, A: 255}
	// Active indicators
	indicator1 := canvas.NewRectangle(activeColor)
	indicator1.SetMinSize(fyne.NewSize(0, 3))
	indicator2 := canvas.NewRectangle(color.Transparent)
	indicator2.SetMinSize(fyne.NewSize(0, 3))
	indicator3 := canvas.NewRectangle(color.Transparent)
	indicator3.SetMinSize(fyne.NewSize(0, 3))
	indicator4 := canvas.NewRectangle(color.Transparent)
	indicator4.SetMinSize(fyne.NewSize(0, 3))
	indicator5 := canvas.NewRectangle(color.Transparent)
	indicator5.SetMinSize(fyne.NewSize(0, 3))
	indicator6 := canvas.NewRectangle(color.Transparent)
	indicator6.SetMinSize(fyne.NewSize(0, 3))
	indicator7 := canvas.NewRectangle(color.Transparent)
	indicator7.SetMinSize(fyne.NewSize(0, 3))
	indicator8 := canvas.NewRectangle(color.Transparent)
	indicator8.SetMinSize(fyne.NewSize(0, 3))

	indicators := []*canvas.Rectangle{indicator1, indicator2, indicator3, indicator4, indicator5, indicator6, indicator7, indicator8}
	contents := []fyne.CanvasObject{updateContent, scanContent, shutdownContent, networkContent, sysInfoContent, configContent, regionContent, authorContent}

	updateTabs := func(active int) {
		for i, ind := range indicators {
			if i == active {
				ind.FillColor = activeColor
			} else {
				ind.FillColor = color.Transparent
			}
			ind.Refresh()
		}
		contentStack.Objects = []fyne.CanvasObject{contents[active]}
		contentStack.Refresh()
	}

	// Create tab buttons with emojis for color (Fyne icons are grayscale)
	tab1Btn := widget.NewButton("⬇️ Update", func() { updateTabs(0) })
	tab2Btn := widget.NewButton("🔍 Quét", func() { updateTabs(1) })
	tab3Btn := widget.NewButton("⏰ Timer", func() { updateTabs(2) })
	tab4Btn := widget.NewButton("💾 Ổ đĩa", func() { updateTabs(3) })
	tab5Btn := widget.NewButton("📊 Info", func() { updateTabs(4) })
	tab6Btn := widget.NewButton("⚙️ INI", func() { updateTabs(5) })
	tab7Btn := widget.NewButton("🌐 Region", func() { updateTabs(6) })
	tab8Btn := widget.NewButton("👤 About", func() { updateTabs(7) })

	// Tab containers with indicator
	tab1 := container.NewBorder(nil, indicator1, nil, nil, tab1Btn)
	tab2 := container.NewBorder(nil, indicator2, nil, nil, tab2Btn)
	tab3 := container.NewBorder(nil, indicator3, nil, nil, tab3Btn)
	tab4 := container.NewBorder(nil, indicator4, nil, nil, tab4Btn)
	tab5 := container.NewBorder(nil, indicator5, nil, nil, tab5Btn)
	tab6 := container.NewBorder(nil, indicator6, nil, nil, tab6Btn)
	tab7 := container.NewBorder(nil, indicator7, nil, nil, tab7Btn)
	tab8 := container.NewBorder(nil, indicator8, nil, nil, tab8Btn)

	// Tab bar - 8 columns (2 rows of 4)
	tabRow1 := container.NewGridWithColumns(4, tab1, tab2, tab3, tab4)
	tabRow2 := container.NewGridWithColumns(4, tab5, tab6, tab7, tab8)
	tabBar := container.NewVBox(tabRow1, tabRow2)
	tabBarBg := canvas.NewRectangle(color.NRGBA{R: 250, G: 250, B: 250, A: 255})
	tabBarContainer := container.NewStack(tabBarBg, tabBar)

	// Main layout
	topSection := container.NewVBox(header, tabBarContainer)

	return container.NewBorder(topSection, footer, nil, nil, container.NewPadded(contentStack))
}

func tabScan() fyne.CanvasObject {
	statusLbl = widget.NewLabel("Sẵn sàng")
	countLbl = widget.NewLabel("0 file")

	fileList = widget.NewList(
		func() int { return len(files) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, widget.NewCheck("", nil), nil, widget.NewLabel(""))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(files) {
				return
			}
			f := files[i]
			c := o.(*fyne.Container)
			c.Objects[0].(*widget.Check).SetChecked(f.selected)
			c.Objects[0].(*widget.Check).OnChanged = func(b bool) { f.selected = b }
			p := f.info.Path
			if len(p) > 38 {
				p = "..." + p[len(p)-35:]
			}
			c.Objects[1].(*widget.Label).SetText(fmt.Sprintf("%s (%s)", p, fmtSize(f.info.Size)))
		},
	)

	scanBtn := widget.NewButton("Quét", func() {
		mutex.Lock()
		if scanning {
			mutex.Unlock()
			return
		}
		scanning = true
		stopChan = make(chan struct{})
		mutex.Unlock()

		files = nil
		fileList.Refresh()
		statusLbl.SetText("Đang quét...")
		countLbl.SetText("—")

		go func() {
			t := time.Now()
			r := scanFiles(stopChan)
			mutex.Lock()
			scanning = false
			mutex.Unlock()
			files = make([]*FileItem, len(r))
			for i, f := range r {
				files[i] = &FileItem{info: f}
			}
			if len(files) == 0 {
				statusLbl.SetText("Không tìm thấy file IPCAS2.ini ẩn")
				countLbl.SetText("0 file")
			} else {
				statusLbl.SetText(fmt.Sprintf("Hoàn tất (%.1fs)", time.Since(t).Seconds()))
				countLbl.SetText(fmt.Sprintf("%d file", len(files)))
			}
			fileList.Refresh()
		}()
	})

	stopBtn := widget.NewButton("Dừng", func() {
		mutex.Lock()
		defer mutex.Unlock()
		if scanning && stopChan != nil {
			close(stopChan)
			scanning = false
			statusLbl.SetText("Đã dừng")
		}
	})

	selBtn := widget.NewButton("Chọn tất cả", func() {
		all := true
		for _, f := range files {
			if !f.selected {
				all = false
				break
			}
		}
		for _, f := range files {
			f.selected = !all
		}
		fileList.Refresh()
	})

	delBtn := widget.NewButton("Xóa đã chọn", func() {
		cnt := 0
		for _, f := range files {
			if f.selected {
				cnt++
			}
		}
		if cnt == 0 {
			showMsg("Thông báo", "Vui lòng chọn file trước")
			return
		}
		showConfirm("Xác nhận xóa", fmt.Sprintf("Bạn có chắc muốn xóa %d file?", cnt), func() {
			del := 0
			for _, f := range files {
				if f.selected && delFile(f.info.Path) == nil {
					del++
				}
			}
			var nf []*FileItem
			for _, f := range files {
				if !f.selected {
					nf = append(nf, f)
				}
			}
			files = nf
			countLbl.SetText(fmt.Sprintf("%d file", len(files)))
			fileList.Refresh()
			showMsg("Hoàn tất", fmt.Sprintf("Đã xóa %d file thành công", del))
		})
	})

	return container.NewBorder(
		container.NewVBox(
			container.NewGridWithColumns(2, scanBtn, stopBtn),
			container.NewGridWithColumns(2, selBtn, delBtn),
			container.NewHBox(statusLbl, widget.NewLabel("•"), countLbl),
		),
		nil, nil, nil, fileList,
	)
}

func tabShutdown() fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Nhập số phút (VD: 30)...")
	sdLabel = widget.NewLabel("Chưa có hẹn giờ")

	return container.NewVBox(
		widget.NewLabel("Thời gian tự động tắt máy:"),
		entry,
		container.NewGridWithColumns(2,
			widget.NewButton("Đặt hẹn giờ", func() {
				m, _ := strconv.Atoi(strings.TrimSpace(entry.Text))
				if m <= 0 {
					showMsg("Lỗi", "Vui lòng nhập số phút hợp lệ")
					return
				}
				exec.Command("shutdown", "/s", "/t", strconv.Itoa(m*60)).Run()
				hasShutdown = true
				sdLabel.SetText("Máy sẽ tắt lúc " + time.Now().Add(time.Duration(m)*time.Minute).Format("15:04"))
			}),
			widget.NewButton("Hủy hẹn giờ", func() {
				if !hasShutdown {
					showMsg("Thông báo", "Chưa có hẹn giờ nào")
					return
				}
				exec.Command("shutdown", "/a").Run()
				hasShutdown = false
				sdLabel.SetText("Đã hủy hẹn giờ")
			}),
		),
		widget.NewSeparator(),
		sdLabel,
	)
}

func tabNetwork() fyne.CanvasObject {
	pathE := widget.NewEntry()
	pathE.SetPlaceHolder("\\\\192.168.1.x\\shared")
	driveS := widget.NewSelect([]string{"Z:", "Y:", "X:", "W:", "V:", "U:", "T:"}, nil)
	driveS.SetSelected("Z:")

	// Cleanup path entry
	cleanPathE := widget.NewEntry()
	cleanPathE.SetPlaceHolder("VD: U:\\ hoặc \\\\10.32.128.12\\Picture")
	cleanPathE.SetText("U:\\")

	cleanStatusLbl := widget.NewLabel("—")
	cleanStatusLbl.Wrapping = fyne.TextWrapWord

	// List to show mapped drives with details
	mappedList := widget.NewList(
		func() int { return 0 },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {},
	)

	var mappings []string

	refresh := func() {
		mappings = nil
		for _, d := range "ZYXWVUT" {
			drv := string(d) + ":"
			out, _ := exec.Command("net", "use", drv).CombinedOutput()
			outStr := string(out)
			if strings.Contains(outStr, "\\\\") {
				// Extract remote path
				lines := strings.Split(outStr, "\n")
				for _, line := range lines {
					if strings.Contains(line, "Remote name") || strings.Contains(line, "Tên từ xa") {
						parts := strings.SplitN(line, "  ", 2)
						if len(parts) >= 2 {
							remotePath := strings.TrimSpace(parts[len(parts)-1])
							mappings = append(mappings, fmt.Sprintf("%s → %s", drv, remotePath))
						}
					}
				}
				if len(mappings) == 0 || !strings.HasPrefix(mappings[len(mappings)-1], drv) {
					// Fallback: just show drive letter
					mappings = append(mappings, drv+" → (đã kết nối)")
				}
			}
		}

		mappedList.Length = func() int { return len(mappings) }
		mappedList.UpdateItem = func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < len(mappings) {
				o.(*widget.Label).SetText(mappings[i])
			}
		}
		mappedList.Refresh()
	}
	refresh()

	// Cleanup junk files function
	cleanupJunk := func() {
		path := strings.TrimSpace(cleanPathE.Text)
		if path == "" {
			showMsg("Lỗi", "Vui lòng nhập đường dẫn thư mục Picture")
			return
		}

		cleanStatusLbl.SetText("Đang quét...")

		go func() {
			today := time.Now().Format("2006-01-02")
			var deleted, scanned int
			var deletedFiles []string

			err := filepath.WalkDir(path, func(filePath string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // Skip errors
				}
				if d.IsDir() {
					return nil
				}

				scanned++
				ext := strings.ToLower(filepath.Ext(filePath))
				name := strings.ToLower(d.Name())

				shouldDelete := false
				reason := ""

				// Delete .env, .enk files
				if ext == ".env" || ext == ".enk" {
					shouldDelete = true
					reason = "file rác"
				}

				// Delete .jpg files not modified today
				if ext == ".jpg" || ext == ".jpeg" {
					if info, err := d.Info(); err == nil {
						modDate := info.ModTime().Format("2006-01-02")
						if modDate != today {
							shouldDelete = true
							reason = "ảnh cũ (" + modDate + ")"
						}
					}
				}

				// Also delete common junk files
				if name == "thumbs.db" || name == "desktop.ini" || ext == ".db" {
					shouldDelete = true
					reason = "file hệ thống"
				}

				if shouldDelete {
					if os.Remove(filePath) == nil {
						deleted++
						// Only keep last 10 for display
						if len(deletedFiles) < 10 {
							deletedFiles = append(deletedFiles, fmt.Sprintf("%s (%s)", d.Name(), reason))
						}
					}
				}

				return nil
			})

			if err != nil {
				cleanStatusLbl.SetText("Lỗi: " + err.Error())
				return
			}

			if deleted == 0 {
				cleanStatusLbl.SetText(fmt.Sprintf("✅ Đã quét %d file, không có file rác", scanned))
			} else {
				result := fmt.Sprintf("🗑️ Đã xóa %d/%d file:\n", deleted, scanned)
				for _, f := range deletedFiles {
					result += "• " + f + "\n"
				}
				if deleted > 10 {
					result += fmt.Sprintf("... và %d file khác", deleted-10)
				}
				cleanStatusLbl.SetText(result)
			}
		}()
	}

	// Scan only (preview)
	scanJunk := func() {
		path := strings.TrimSpace(cleanPathE.Text)
		if path == "" {
			showMsg("Lỗi", "Vui lòng nhập đường dẫn thư mục Picture")
			return
		}

		cleanStatusLbl.SetText("Đang quét...")

		go func() {
			today := time.Now().Format("2006-01-02")
			var junkCount, scanned int
			var junkFiles []string

			filepath.WalkDir(path, func(filePath string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}

				scanned++
				ext := strings.ToLower(filepath.Ext(filePath))
				name := strings.ToLower(d.Name())

				isJunk := false
				reason := ""

				if ext == ".evn" || ext == ".enk" {
					isJunk = true
					reason = "file rác"
				}

				if ext == ".jpg" || ext == ".jpeg" {
					if info, err := d.Info(); err == nil {
						modDate := info.ModTime().Format("2006-01-02")
						if modDate != today {
							isJunk = true
							reason = "ảnh cũ"
						}
					}
				}

				if name == "thumbs.db" || name == "desktop.ini" || ext == ".db" {
					isJunk = true
					reason = "file hệ thống"
				}

				if isJunk {
					junkCount++
					if len(junkFiles) < 10 {
						junkFiles = append(junkFiles, fmt.Sprintf("%s (%s)", d.Name(), reason))
					}
				}

				return nil
			})

			if junkCount == 0 {
				cleanStatusLbl.SetText(fmt.Sprintf("✅ Đã quét %d file, không có file rác", scanned))
			} else {
				result := fmt.Sprintf("⚠️ Tìm thấy %d file rác trong %d file:\n", junkCount, scanned)
				for _, f := range junkFiles {
					result += "• " + f + "\n"
				}
				if junkCount > 10 {
					result += fmt.Sprintf("... và %d file khác", junkCount-10)
				}
				cleanStatusLbl.SetText(result)
			}
		}()
	}

	return container.NewScroll(container.NewVBox(
		widget.NewLabel("📂 Kết nối ổ mạng"),
		widget.NewLabel("Đường dẫn mạng:"), pathE,
		widget.NewLabel("Chọn ổ đĩa:"), driveS,
		container.NewGridWithColumns(2,
			widget.NewButton("Kết nối", func() {
				p := strings.TrimSpace(pathE.Text)
				if p == "" {
					showMsg("Lỗi", "Vui lòng nhập đường dẫn mạng")
					return
				}
				out, e := exec.Command("net", "use", driveS.Selected, p, "/persistent:yes").CombinedOutput()
				if e != nil {
					showMsg("Lỗi", string(out))
					return
				}
				showMsg("Thành công", "Đã kết nối "+driveS.Selected+" → "+p)
				refresh()
			}),
			widget.NewButton("Ngắt kết nối", func() {
				exec.Command("net", "use", driveS.Selected, "/delete", "/yes").Run()
				showMsg("Thành công", "Đã ngắt "+driveS.Selected)
				refresh()
			}),
		),
		widget.NewSeparator(),
		widget.NewLabel("🗑️ Dọn file rác (Picture)"),
		widget.NewLabel("Đường dẫn thư mục:"), cleanPathE,
		widget.NewLabel("Xóa: .env, .enk, Thumbs.db, ảnh .jpg cũ (không phải hôm nay)"),
		container.NewGridWithColumns(2,
			widget.NewButton("🔍 Quét (xem trước)", scanJunk),
			widget.NewButton("🗑️ Xóa file rác", func() {
				showConfirm("Xác nhận xóa", "Bạn có chắc muốn xóa tất cả file rác?\n(.evn, .enk, Thumbs.db, ảnh cũ)", cleanupJunk)
			}),
		),
		cleanStatusLbl,
		widget.NewSeparator(),
		widget.NewLabel("Ổ mạng đã kết nối:"),
		mappedList,
	))
}

// System Info tab - MAC, Ping, Hostname
func tabSystemInfo() fyne.CanvasObject {
	// Colors for UI
	blueColor := color.NRGBA{R: 0, G: 103, B: 192, A: 255}
	greenColor := color.NRGBA{R: 0, G: 150, B: 80, A: 255}

	// Helper to create info card
	makeInfoCard := func(title string, titleColor color.Color) (*canvas.Text, *widget.Label, fyne.CanvasObject) {
		bg := canvas.NewRectangle(color.White)
		bg.CornerRadius = 8

		t := canvas.NewText(title, titleColor)
		t.TextSize = 12
		t.TextStyle = fyne.TextStyle{Bold: true}

		v := widget.NewLabel("—")
		v.Wrapping = fyne.TextWrapWord

		content := container.NewVBox(t, v)
		card := container.NewStack(bg, container.NewPadded(content))
		return t, v, card
	}

	// Hostname
	_, hostnameValue, hostnameCard := makeInfoCard("🖥️ Tên máy tính", blueColor)

	// MAC Address
	_, macValue, macCard := makeInfoCard("🔌 Địa chỉ MAC (LAN)", greenColor)

	// IP Address
	_, ipValue, ipCard := makeInfoCard("🌐 Địa chỉ IP", blueColor)

	// Ping result
	pingEntry := widget.NewEntry()
	pingEntry.SetPlaceHolder("Nhập IP hoặc hostname (VD: 10.0.91.10)")
	pingResult := widget.NewLabel("—")
	pingResult.Wrapping = fyne.TextWrapWord

	// Get system info
	refreshInfo := func() {
		// Hostname
		if name, err := os.Hostname(); err == nil {
			hostnameValue.SetText(name)
		}

		// Get network interfaces
		interfaces, err := net.Interfaces()
		if err == nil {
			var macs, ips []string
			for _, iface := range interfaces {
				// Skip loopback and virtual adapters
				if iface.Flags&net.FlagLoopback != 0 {
					continue
				}
				if iface.HardwareAddr == nil || len(iface.HardwareAddr) == 0 {
					continue
				}
				// Filter for Ethernet/LAN adapters
				nameLower := strings.ToLower(iface.Name)
				if strings.Contains(nameLower, "ethernet") ||
					strings.Contains(nameLower, "local") ||
					strings.Contains(nameLower, "lan") ||
					!strings.Contains(nameLower, "virtual") {
					mac := iface.HardwareAddr.String()
					if mac != "" {
						macs = append(macs, fmt.Sprintf("%s: %s", iface.Name, mac))
					}

					// Get IP addresses
					addrs, _ := iface.Addrs()
					for _, addr := range addrs {
						if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
							if ipnet.IP.To4() != nil {
								ips = append(ips, fmt.Sprintf("%s: %s", iface.Name, ipnet.IP.String()))
							}
						}
					}
				}
			}
			if len(macs) > 0 {
				macValue.SetText(strings.Join(macs, "\n"))
			} else {
				macValue.SetText("Không tìm thấy")
			}
			if len(ips) > 0 {
				ipValue.SetText(strings.Join(ips, "\n"))
			} else {
				ipValue.SetText("Không tìm thấy")
			}
		}
	}

	// Ping function
	doPing := func() {
		target := strings.TrimSpace(pingEntry.Text)
		if target == "" {
			pingResult.SetText("Vui lòng nhập địa chỉ IP hoặc hostname")
			return
		}
		pingResult.SetText("Đang ping...")

		go func() {
			out, err := exec.Command("ping", "-n", "3", target).CombinedOutput()
			if err != nil {
				pingResult.SetText("❌ Không thể kết nối: " + target)
				return
			}
			// Parse ping result
			outStr := string(out)
			if strings.Contains(outStr, "TTL=") || strings.Contains(outStr, "ttl=") {
				pingResult.SetText("✅ Ping thành công!\n" + target + " đang hoạt động")
			} else {
				pingResult.SetText("❌ Không phản hồi: " + target)
			}
		}()
	}

	// Initial load
	go func() {
		time.Sleep(200 * time.Millisecond)
		refreshInfo()
	}()

	// === DOMAIN JOIN SECTION ===
	currentNameLbl := widget.NewLabel("Tên máy hiện tại: —")
	currentDomainLbl := widget.NewLabel("Domain: —")
	currentDomainLbl.Wrapping = fyne.TextWrapWord

	// Refresh domain info
	refreshDomainInfo := func() {
		// Get current computer name
		if name, err := os.Hostname(); err == nil {
			currentNameLbl.SetText("Tên máy hiện tại: " + name)
		}

		// Check domain status using PowerShell
		go func() {
			cmd := exec.Command("powershell", "-Command", "(Get-WmiObject Win32_ComputerSystem).Domain")
			out, err := cmd.CombinedOutput()
			if err != nil {
				currentDomainLbl.SetText("Domain: (Không thể kiểm tra)")
				return
			}
			domain := strings.TrimSpace(string(out))
			if domain == "" || strings.ToLower(domain) == "workgroup" {
				currentDomainLbl.SetText("Domain: ❌ Chưa join (Workgroup)")
			} else {
				currentDomainLbl.SetText("Domain: ✅ " + domain)
			}
		}()
	}

	// Initial domain check
	go func() {
		time.Sleep(400 * time.Millisecond)
		refreshDomainInfo()
	}()

	branchEntry := widget.NewEntry()
	branchEntry.SetPlaceHolder("Mã chi nhánh (VD: 3611)")
	branchEntry.SetText("3611")

	dnsEntry := widget.NewEntry()
	dnsEntry.SetPlaceHolder("DNS Server (VD: 10.0.58.11)")
	dnsEntry.SetText("10.0.58.11")

	domainEntry := widget.NewEntry()
	domainEntry.SetPlaceHolder("Tên domain")
	domainEntry.SetText("corp.agribank.com.vn")

	domainUser := widget.NewEntry()
	domainUser.SetPlaceHolder("Tài khoản domain (VD: admin)")

	domainPass := widget.NewPasswordEntry()
	domainPass.SetPlaceHolder("Mật khẩu")

	domainStatus := widget.NewLabel("—")
	domainStatus.Wrapping = fyne.TextWrapWord

	suggestedName := widget.NewLabel("Tên máy đề xuất: —")

	// Auto-generate computer name from IP
	generateComputerName := func() string {
		interfaces, err := net.Interfaces()
		if err != nil {
			return ""
		}
		branch := strings.TrimSpace(branchEntry.Text)
		if branch == "" {
			branch = "XXXX"
		}

		for _, iface := range interfaces {
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
					if ip4 := ipnet.IP.To4(); ip4 != nil {
						// Get last 6 digits: xxx.xxx.YYY.ZZZ -> YYYZZZ
						ipStr := ip4.String()
						parts := strings.Split(ipStr, ".")
						if len(parts) == 4 {
							// Format: 3rd octet (3 digits) + 4th octet (3 digits)
							third := fmt.Sprintf("%03s", parts[2])
							fourth := fmt.Sprintf("%03s", parts[3])
							// Take last 3 of each
							if len(third) > 3 {
								third = third[len(third)-3:]
							}
							if len(fourth) > 3 {
								fourth = fourth[len(fourth)-3:]
							}
							return fmt.Sprintf("%s-WS%s%s", branch, third, fourth)
						}
					}
				}
			}
		}
		return branch + "-WS000000"
	}

	// Update suggested name when branch changes
	updateSuggested := func() {
		name := generateComputerName()
		suggestedName.SetText("Tên máy đề xuất: " + name)
	}
	branchEntry.OnChanged = func(s string) {
		updateSuggested()
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		updateSuggested()
	}()

	// Rename computer
	renameComputer := func() {
		newName := generateComputerName()
		if newName == "" || strings.Contains(newName, "XXXX") {
			showMsg("Lỗi", "Vui lòng nhập mã chi nhánh")
			return
		}

		domainStatus.SetText("Đang đổi tên máy...")

		go func() {
			// Use PowerShell to rename computer
			cmd := exec.Command("powershell", "-Command",
				fmt.Sprintf(`Rename-Computer -NewName "%s" -Force`, newName))
			out, err := cmd.CombinedOutput()
			if err != nil {
				domainStatus.SetText("❌ Lỗi đổi tên: " + string(out))
				return
			}
			domainStatus.SetText("✅ Đã đổi tên máy thành: " + newName + "\n⚠️ Cần restart để có hiệu lực!")
			showMsg("Thành công", "Đã đổi tên máy thành:\n"+newName+"\n\nCần restart máy!")
		}()
	}

	// Set DNS
	setDNS := func() {
		dns := strings.TrimSpace(dnsEntry.Text)
		if dns == "" {
			showMsg("Lỗi", "Vui lòng nhập DNS Server")
			return
		}

		domainStatus.SetText("Đang cài đặt DNS...")

		go func() {
			// Set DNS on Ethernet adapter
			cmd := exec.Command("powershell", "-Command",
				fmt.Sprintf(`Set-DnsClientServerAddress -InterfaceAlias "Ethernet" -ServerAddresses "%s"`, dns))
			out, err := cmd.CombinedOutput()
			if err != nil {
				// Try with "Ethernet0" or other adapters
				cmd2 := exec.Command("netsh", "interface", "ip", "set", "dns",
					"name=Ethernet", "static", dns)
				out2, err2 := cmd2.CombinedOutput()
				if err2 != nil {
					domainStatus.SetText("❌ Lỗi đặt DNS: " + string(out) + "\n" + string(out2))
					return
				}
			}
			domainStatus.SetText("✅ Đã đặt DNS: " + dns)
		}()
	}

	// Join domain
	joinDomain := func() {
		domain := strings.TrimSpace(domainEntry.Text)
		user := strings.TrimSpace(domainUser.Text)
		pass := domainPass.Text

		if domain == "" || user == "" || pass == "" {
			showMsg("Lỗi", "Vui lòng điền đầy đủ:\n- Domain\n- Tài khoản\n- Mật khẩu")
			return
		}

		showConfirm("Xác nhận Join Domain",
			fmt.Sprintf("Bạn có chắc muốn join vào domain:\n%s\n\nMáy sẽ restart sau khi join!", domain),
			func() {
				domainStatus.SetText("Đang join domain...")

				go func() {
					// PowerShell command to join domain
					psCommand := fmt.Sprintf(
						`$password = ConvertTo-SecureString "%s" -AsPlainText -Force; `+
							`$cred = New-Object System.Management.Automation.PSCredential("%s@%s", $password); `+
							`Add-Computer -DomainName "%s" -Credential $cred -Force -Restart`,
						pass, user, domain, domain)

					cmd := exec.Command("powershell", "-Command", psCommand)
					out, err := cmd.CombinedOutput()
					if err != nil {
						domainStatus.SetText("❌ Lỗi join domain:\n" + string(out))
						return
					}
					domainStatus.SetText("✅ Đã join domain thành công!\nMáy sẽ restart...")
				}()
			})
	}

	return container.NewScroll(container.NewVBox(
		widget.NewLabel("📊 Thông tin hệ thống"),
		hostnameCard,
		macCard,
		ipCard,
		widget.NewSeparator(),
		widget.NewLabel("🔍 Kiểm tra kết nối (Ping)"),
		pingEntry,
		widget.NewButton("Ping", doPing),
		pingResult,
		widget.NewSeparator(),
		widget.NewLabel("🖥️ Đổi tên máy & Join Domain"),
		currentNameLbl,
		currentDomainLbl,
		widget.NewButton("🔄 Kiểm tra trạng thái", refreshDomainInfo),
		widget.NewSeparator(),
		widget.NewLabel("Mã chi nhánh:"), branchEntry,
		suggestedName,
		widget.NewButton("✏️ Đổi tên máy", renameComputer),
		widget.NewSeparator(),
		widget.NewLabel("DNS Server:"), dnsEntry,
		widget.NewButton("🌐 Đặt DNS", setDNS),
		widget.NewSeparator(),
		widget.NewLabel("Domain:"), domainEntry,
		widget.NewLabel("Tài khoản:"), domainUser,
		widget.NewLabel("Mật khẩu:"), domainPass,
		widget.NewButton("🔗 Join Domain", joinDomain),
		domainStatus,
		widget.NewSeparator(),
		widget.NewButton("🔄 Tải lại thông tin", refreshInfo),
	))
}

func tabAuthor() fyne.CanvasObject {
	name := canvas.NewText("Phan Tiến", color.NRGBA{R: 0, G: 103, B: 192, A: 255})
	name.TextSize = 20
	name.Alignment = fyne.TextAlignCenter

	org := canvas.NewText("Agribank Chi nhánh Tây Nghệ An", color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	org.TextSize = 13
	org.Alignment = fyne.TextAlignCenter

	makeCard := func(title, value, sub string) fyne.CanvasObject {
		bg := canvas.NewRectangle(color.White)
		bg.CornerRadius = 8

		t := canvas.NewText(title, color.NRGBA{R: 130, G: 130, B: 130, A: 255})
		t.TextSize = 11
		t.Alignment = fyne.TextAlignCenter

		v := canvas.NewText(value, color.NRGBA{R: 0, G: 103, B: 192, A: 255})
		v.TextSize = 18
		v.Alignment = fyne.TextAlignCenter

		s := canvas.NewText(sub, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
		s.TextSize = 11
		s.Alignment = fyne.TextAlignCenter

		content := container.NewVBox(
			container.NewCenter(t),
			container.NewCenter(v),
			container.NewCenter(s),
		)
		return container.NewStack(bg, container.NewPadded(content))
	}

	// Feature descriptions
	featuresText := widget.NewLabel(
		"📋 HƯỚNG DẪN SỬ DỤNG:\n\n" +
			"⬇️ Update - Cập nhật IPCAS2 từ server\n" +
			"🔍 Quét - Tìm & xóa file IPCAS2.ini ẩn\n" +
			"⏰ Timer - Hẹn giờ tắt máy tự động\n" +
			"💾 Ổ đĩa - Map ổ mạng & dọn file rác\n" +
			"📊 Info - Xem MAC, IP, Ping test\n" +
			"⚙️ INI - Cấu hình IPCAS2.ini\n" +
			"🌐 Region - Cài đặt định dạng ngày/số\n" +
			"👤 About - Thông tin tác giả")
	featuresText.Wrapping = fyne.TextWrapWord

	return container.NewScroll(container.NewVBox(
		widget.NewLabel(""),
		container.NewCenter(name),
		container.NewCenter(org),
		widget.NewLabel(""),
		makeCard("Ủng hộ tác giả", "3611205088888", "Ngân hàng Agribank"),
		makeCard("Liên hệ hỗ trợ", "0945626999", "Điện thoại / Zalo"),
		widget.NewSeparator(),
		featuresText,
	))
}

const ipcasIniPath = `C:\Windows\IPCAS2.ini`
const ipcasTemplate = `[TUXEDO]
tuxdir=C:\TUXEDO

eiini=C:\ipcas2\INI
appdir=C:\ipcas2\Bin
fldtbldir32=C:\TUXEDO\UDATAOBJ;C:\IPCAS2\fmldir
fieldtbls32=usysfl32,rcfldtbl.tux,race.fld,keb.fld,tpadm
ulogpfx=c:\ipcas2\TUXLOG\ulog

[IPCAS2]
sys_brcd=%s
UseUnicodeEncoding=Y
usrflg=ON
cacheflag=N
;1:Top, 2:Left, 3:Both
SHOWMENU=3
;Y:Yes, N:No, A:Auto
NormVNMenu = N

[TEST]
;wsnaddr=//10.0.91.10:10000

[LIVE]
wsnaddr=//10.0.91.10:10000

[KEBTMP]
KEBTMP=kebtmp.ini

[KEBMSG]
KEBMSG=C:\ipcas2\Msg\

[KEBSIGN]
CMSIGN=C:\ipcas2\sign

[KEBPICTURE]
PICTURE=U:\

[KEBRPT]
RPT=C:\ipcas2
[ONPRT]
PRT=c:\ipcas2\TEMPLATE\PRINT
;O = Other, using Windows Printing System, S = Synkey(Raw device), R = Synkey(Generic Device), D = Datawindow, Defaul = O

PRTYPE =O
;V = Viet Nam, E = English, Defaul = Viet Nam
;Physical Offset
PHYOFFX=0.2
PHYOFFY=0.2
PRLANG =V

;Printer Name - in case of Windows Printing System
;Open Print Manager to retrieve Printer Name
;Network Printer:\\[hostname or ipaddress]\Printer Name(Printer Name on hostname or ipaddress)
;Local Printer:Printer Name
PRNAME=INSOTK
[LANGUAGE]
LANG=C:\ipcas2\INCLUDE\

[CACHE]
CACHE=C:\ipcas2\CACHE\

[ONOFFLINE]
SYS_LONG02=1
SYS_LONG04=1

[KEBPASSBOOK]
PORT=1

[TOKENSETUP]
ACTIVE=%s
TOKEN1=./SecureMetric_PKI_csp11.dll
TOKEN2=./eToken.dll
TOKEN3=./acospkcs11.dll
TOKEN4=./st3csp11.dll
TOKEN5=./dkck201.dll
TOKEN6=./gclib.dll
TOKEN7=./agribank_csp11_v1.dll
`

func tabConfig() fyne.CanvasObject {
	statusLabel := widget.NewLabel("Đang kiểm tra...")

	// sys_brcd options: 3611-3620 excluding 3615
	brcdOptions := []string{"3611", "3612", "3613", "3614", "3616", "3617", "3618", "3619", "3620"}
	brcdSelect := widget.NewSelect(brcdOptions, nil)
	brcdSelect.SetSelected("3611")

	// Token options with descriptions
	tokenOptions := []string{
		"TOKEN1 - SecureMetric PKI",
		"TOKEN2 - USB Đỏ (eToken)",
		"TOKEN3 - ACOS",
		"TOKEN4 - ST3",
		"TOKEN5 - DKCK",
		"TOKEN6 - Thẻ PKI Smart Card",
		"TOKEN7 - USB Đen (Agribank)",
	}
	tokenSelect := widget.NewSelect(tokenOptions, nil)
	tokenSelect.SetSelected("TOKEN7 - USB Đen (Agribank)")

	// Read current config
	readConfig := func() {
		data, err := os.ReadFile(ipcasIniPath)
		if err != nil {
			statusLabel.SetText("❌ File chưa tồn tại")
			return
		}
		content := string(data)

		// Find sys_brcd
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "sys_brcd=") {
				val := strings.TrimPrefix(line, "sys_brcd=")
				val = strings.TrimPrefix(val, "SYS_BRCD=")
				for _, opt := range brcdOptions {
					if strings.Contains(val, opt) {
						brcdSelect.SetSelected(opt)
						break
					}
				}
			}
			if strings.HasPrefix(strings.ToUpper(line), "ACTIVE=") {
				val := strings.TrimSpace(strings.Split(line, "=")[1])
				for _, opt := range tokenOptions {
					if strings.HasPrefix(opt, val) {
						tokenSelect.SetSelected(opt)
						break
					}
				}
			}
		}
		statusLabel.SetText("✅ Đã tải cấu hình")
	}

	// Save config
	saveConfig := func() {
		brcd := brcdSelect.Selected
		tokenFull := tokenSelect.Selected
		token := strings.Split(tokenFull, " ")[0]

		content := fmt.Sprintf(ipcasTemplate, brcd, token)

		// Write file
		err := os.WriteFile(ipcasIniPath, []byte(content), 0666)
		if err != nil {
			// Try with elevated - create in temp first
			showMsg("Lỗi", "Không thể ghi file. Chạy với quyền Admin!")
			return
		}

		// Set full permissions
		exec.Command("icacls", ipcasIniPath, "/grant", "Everyone:F").Run()

		showMsg("Thành công", "Đã lưu cấu hình IPCAS2.ini")
		readConfig()
	}

	// Create file if not exists
	createFile := func() {
		if _, err := os.Stat(ipcasIniPath); err == nil {
			showMsg("Thông báo", "File đã tồn tại")
			return
		}
		saveConfig()
	}

	// Create desktop shortcut
	createShortcut := func() {
		desktop := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
		shortcutPath := filepath.Join(desktop, "IPCAS2.lnk")
		targetPath := `C:\IPCAS2\Bin\ipcas2.exe`

		// Check if target exists
		if _, err := os.Stat(targetPath); err != nil {
			showMsg("Lỗi", "Không tìm thấy ipcas2.exe")
			return
		}

		// Use PowerShell to create shortcut
		psScript := fmt.Sprintf(`$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut('%s'); $s.TargetPath = '%s'; $s.WorkingDirectory = 'C:\IPCAS2\Bin'; $s.Save()`, shortcutPath, targetPath)
		cmd := exec.Command("powershell", "-Command", psScript)
		err := cmd.Run()
		if err != nil {
			showMsg("Lỗi", "Không thể tạo shortcut")
			return
		}
		showMsg("Thành công", "Đã tạo shortcut IPCAS2 trên Desktop")
	}

	// Run initsign as admin
	runInitsign := func() {
		initsignPath := `C:\IPCAS2\Bin\initsign.exe`
		if _, err := os.Stat(initsignPath); err != nil {
			showMsg("Lỗi", "Không tìm thấy initsign.exe")
			return
		}

		// Run as admin using runas
		cmd := exec.Command("powershell", "Start-Process", "-FilePath", initsignPath, "-Verb", "RunAs", "-WorkingDirectory", `C:\IPCAS2\Bin`)
		err := cmd.Start()
		if err != nil {
			showMsg("Lỗi", "Không thể chạy initsign.exe")
			return
		}
		showMsg("Thành công", "Đã chạy initsign.exe với quyền Admin")
	}

	// Fix sign folder
	fixSignFolder := func() {
		signPath := `C:\IPCAS2\sign`
		if _, err := os.Stat(signPath); err == nil {
			showMsg("Thông báo", "Thư mục sign đã tồn tại")
			return
		}

		err := os.MkdirAll(signPath, 0755)
		if err != nil {
			showMsg("Lỗi", "Không thể tạo thư mục sign. Chạy với quyền Admin!")
			return
		}

		// Set full permissions
		exec.Command("icacls", signPath, "/grant", "Everyone:F").Run()
		showMsg("Thành công", "Đã tạo thư mục C:\\IPCAS2\\sign")
	}

	// Initial read
	go func() {
		time.Sleep(200 * time.Millisecond)
		readConfig()
	}()

	return container.NewVBox(
		widget.NewLabel("📋 Cấu hình IPCAS2.ini"),
		statusLabel,
		widget.NewSeparator(),
		widget.NewLabel("Mã chi nhánh (sys_brcd):"),
		brcdSelect,
		widget.NewLabel("Loại Token (ACTIVE):"),
		tokenSelect,
		widget.NewSeparator(),
		container.NewGridWithColumns(2,
			widget.NewButton("💾 Lưu cấu hình", saveConfig),
			widget.NewButton("📄 Tạo file mới", createFile),
		),
		widget.NewButton("🔄 Tải lại", func() { readConfig() }),
		widget.NewSeparator(),
		widget.NewLabel("🔧 Tiện ích IPCAS2:"),
		container.NewGridWithColumns(2,
			widget.NewButton("🖥️ Tạo Shortcut", createShortcut),
			widget.NewButton("🔑 Chạy InitSign", runInitsign),
		),
		widget.NewButton("🔧 Fix lỗi chữ ký (tạo folder sign)", fixSignFolder),
	)
}

func tabRegion() fyne.CanvasObject {
	statusLabel := widget.NewLabel("Kiểm tra cài đặt Region...")

	// Current settings display
	currentDate := widget.NewLabel("Định dạng ngày: —")
	currentDecimal := widget.NewLabel("Dấu thập phân: —")
	currentGroup := widget.NewLabel("Dấu phân cách nghìn: —")

	// Read current settings from registry
	readCurrentSettings := func() {
		// Read sShortDate
		out, _ := exec.Command("reg", "query", `HKCU\Control Panel\International`, "/v", "sShortDate").CombinedOutput()
		if strings.Contains(string(out), "sShortDate") {
			parts := strings.Fields(string(out))
			if len(parts) >= 3 {
				currentDate.SetText("Định dạng ngày: " + parts[len(parts)-1])
			}
		}

		// Read sDecimal
		out, _ = exec.Command("reg", "query", `HKCU\Control Panel\International`, "/v", "sDecimal").CombinedOutput()
		if strings.Contains(string(out), "sDecimal") {
			parts := strings.Fields(string(out))
			if len(parts) >= 3 {
				currentDecimal.SetText("Dấu thập phân: " + parts[len(parts)-1])
			}
		}

		// Read sThousand
		out, _ = exec.Command("reg", "query", `HKCU\Control Panel\International`, "/v", "sThousand").CombinedOutput()
		if strings.Contains(string(out), "sThousand") {
			parts := strings.Fields(string(out))
			if len(parts) >= 3 {
				currentGroup.SetText("Dấu phân cách nghìn: " + parts[len(parts)-1])
			}
		}

		statusLabel.SetText("✅ Đã đọc cài đặt hiện tại")
	}

	// Apply Vietnam/IPCAS standard format
	applyFormat := func() {
		// Set registry values for IPCAS2 compatible format
		regCommands := [][]string{
			{"reg", "add", `HKCU\Control Panel\International`, "/v", "sShortDate", "/t", "REG_SZ", "/d", "dd/MM/yyyy", "/f"},
			{"reg", "add", `HKCU\Control Panel\International`, "/v", "sLongDate", "/t", "REG_SZ", "/d", "dddd, d MMMM yyyy", "/f"},
			{"reg", "add", `HKCU\Control Panel\International`, "/v", "sDecimal", "/t", "REG_SZ", "/d", ".", "/f"},
			{"reg", "add", `HKCU\Control Panel\International`, "/v", "sThousand", "/t", "REG_SZ", "/d", ",", "/f"},
			{"reg", "add", `HKCU\Control Panel\International`, "/v", "iDate", "/t", "REG_SZ", "/d", "1", "/f"},
			{"reg", "add", `HKCU\Control Panel\International`, "/v", "sDate", "/t", "REG_SZ", "/d", "/", "/f"},
		}

		for _, args := range regCommands {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Run()
		}

		readCurrentSettings()

		showMsg("Thành công", "Đã cập nhật Region Format.\nCần restart máy để IPCAS2 hoạt động đúng.")
	}

	// Initial read
	go func() {
		time.Sleep(300 * time.Millisecond)
		readCurrentSettings()
	}()

	infoText := canvas.NewText("Chuẩn IPCAS2: dd/MM/yyyy, dấu . và ,", color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	infoText.TextSize = 11

	return container.NewVBox(
		widget.NewLabel("🌐 Cài đặt Region Format"),
		statusLabel,
		widget.NewSeparator(),
		widget.NewLabel("Cài đặt hiện tại:"),
		currentDate,
		currentDecimal,
		currentGroup,
		widget.NewSeparator(),
		container.NewCenter(infoText),
		widget.NewButton("Áp dụng chuẩn IPCAS2", applyFormat),
		widget.NewButton("Tải lại", func() { readCurrentSettings() }),
		widget.NewLabel(""),
		widget.NewLabel("Cần restart sau khi áp dụng"),
	)
}

// Update configuration
var updateSourcePath = `\\10.32.128.12\IPCAS2\Bin`
var updateTargetPath = `C:\IPCAS2\Bin`
var updateBackupDir = `C:\IPCAS2\Backup`
var updateConfigFile = `C:\IPCAS2\update_config.txt`

func tabUpdate() fyne.CanvasObject {
	// Load saved config
	if data, err := os.ReadFile(updateConfigFile); err == nil {
		updateSourcePath = strings.TrimSpace(string(data))
	}

	sourceEntry := widget.NewEntry()
	sourceEntry.SetText(updateSourcePath)
	sourceEntry.SetPlaceHolder(`\\server\IPCAS2\Bin`)

	statusLabel := widget.NewLabel("Sẵn sàng")
	progressBar := widget.NewProgressBar()
	progressBar.Hide()

	logText := widget.NewMultiLineEntry()
	logText.SetPlaceHolder("Log cập nhật...")
	logText.Wrapping = fyne.TextWrapWord

	backupList := widget.NewSelect([]string{}, nil)

	addLog := func(msg string) {
		// Limit log to 100 lines for performance
		lines := strings.Split(logText.Text, "\n")
		if len(lines) > 100 {
			lines = lines[len(lines)-100:]
		}
		logText.SetText(strings.Join(lines, "\n") + time.Now().Format("15:04:05") + " - " + msg + "\n")
	}

	// Refresh backup list
	refreshBackups := func() {
		var backups []string
		if entries, err := os.ReadDir(updateBackupDir); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "BK_") && strings.HasSuffix(e.Name(), ".zip") {
					backups = append(backups, e.Name())
				}
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(backups)))
		backupList.Options = backups
		if len(backups) > 0 {
			backupList.SetSelected(backups[0])
		}
		backupList.Refresh()
	}

	// Create backup
	createBackup := func() error {
		os.MkdirAll(updateBackupDir, 0755)

		backupName := fmt.Sprintf("BK_%s.zip", time.Now().Format("20060102_150405"))
		backupPath := filepath.Join(updateBackupDir, backupName)

		addLog("Đang tạo backup: " + backupName)

		zipFile, err := os.Create(backupPath)
		if err != nil {
			return err
		}
		defer zipFile.Close()

		w := zip.NewWriter(zipFile)
		defer w.Close()

		filepath.WalkDir(updateTargetPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			relPath, _ := filepath.Rel(updateTargetPath, path)
			f, err := w.Create(relPath)
			if err != nil {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			f.Write(data)
			return nil
		})

		// Clean old backups (keep max 3)
		entries, _ := os.ReadDir(updateBackupDir)
		var bkFiles []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "BK_") && strings.HasSuffix(e.Name(), ".zip") {
				bkFiles = append(bkFiles, e.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(bkFiles)))
		for i := 3; i < len(bkFiles); i++ {
			os.Remove(filepath.Join(updateBackupDir, bkFiles[i]))
			addLog("Xóa backup cũ: " + bkFiles[i])
		}

		addLog("Backup hoàn tất: " + backupName)
		return nil
	}

	// Calculate MD5 hash of file
	fileHash := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%x", md5.Sum(data))
	}

	// Compare and get files to update (with accurate hash comparison)
	getFilesToUpdate := func() ([]string, error) {
		var toUpdate []string
		var mu sync.Mutex

		err := filepath.WalkDir(updateSourcePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}

			relPath, _ := filepath.Rel(updateSourcePath, path)
			targetFile := filepath.Join(updateTargetPath, relPath)

			srcInfo, _ := d.Info()
			tgtInfo, tgtErr := os.Stat(targetFile)

			needUpdate := false
			if tgtErr != nil {
				needUpdate = true // Target doesn't exist
			} else if srcInfo.Size() != tgtInfo.Size() {
				needUpdate = true // Size different
			} else {
				// Same size - compare hash for accuracy
				srcHash := fileHash(path)
				tgtHash := fileHash(targetFile)
				if srcHash != "" && tgtHash != "" && srcHash != tgtHash {
					needUpdate = true // Content different
				}
			}

			if needUpdate {
				mu.Lock()
				toUpdate = append(toUpdate, relPath)
				mu.Unlock()
			}
			return nil
		})

		return toUpdate, err
	}

	// Kill IPCAS2 process
	killIPCAS := func() {
		exec.Command("taskkill", "/F", "/IM", "ipcas2.exe").Run()
		addLog("Đã tắt ipcas2.exe")
		time.Sleep(500 * time.Millisecond)
	}

	// Launch IPCAS2
	launchIPCAS := func() {
		exe := filepath.Join(updateTargetPath, "ipcas2.exe")
		if _, err := os.Stat(exe); err == nil {
			exec.Command("cmd", "/C", "start", "", exe).Start()
			addLog("Đã mở ipcas2.exe")
		}
	}

	// Save config
	saveConfig := func() {
		updateSourcePath = sourceEntry.Text
		os.MkdirAll(filepath.Dir(updateConfigFile), 0755)
		os.WriteFile(updateConfigFile, []byte(updateSourcePath), 0644)
		addLog("Đã lưu cấu hình")
	}

	// Main update function - runs check in background
	doUpdate := func() {
		saveConfig()

		statusLabel.SetText("Đang kiểm tra...")
		progressBar.Show()
		progressBar.SetValue(0)
		addLog("Bắt đầu kiểm tra từ: " + updateSourcePath)

		// Run file check in background to avoid freezing
		go func() {
			files, err := getFilesToUpdate()

			progressBar.Hide()

			if err != nil {
				addLog("Lỗi: " + err.Error())
				statusLabel.SetText("Lỗi kết nối")
				return
			}

			if len(files) == 0 {
				addLog("Không có file cần cập nhật")
				statusLabel.SetText("✅ Đã cập nhật mới nhất")
				return
			}

			addLog(fmt.Sprintf("Cần cập nhật %d file", len(files)))

			// Inner function to perform file updates
			performFilesUpdate := func() {
				killIPCAS()

				progressBar.Show()
				progressBar.SetValue(0)
				startTime := time.Now()

				for i, relPath := range files {
					srcFile := filepath.Join(updateSourcePath, relPath)
					dstFile := filepath.Join(updateTargetPath, relPath)

					os.MkdirAll(filepath.Dir(dstFile), 0755)

					data, err := os.ReadFile(srcFile)
					if err != nil {
						addLog("Lỗi đọc: " + relPath)
						continue
					}

					if err := os.WriteFile(dstFile, data, 0644); err != nil {
						addLog("Lỗi ghi: " + relPath)
						continue
					}

					addLog("Cập nhật: " + relPath)

					progress := float64(i+1) / float64(len(files))
					progressBar.SetValue(progress)

					elapsed := time.Since(startTime)
					remaining := time.Duration(float64(elapsed) / progress * (1 - progress))
					statusLabel.SetText(fmt.Sprintf("Đang cập nhật... %d/%d (còn ~%s)", i+1, len(files), remaining.Round(time.Second)))
				}

				progressBar.SetValue(1)
				progressBar.Hide()

				addLog(fmt.Sprintf("Hoàn tất cập nhật %d file trong %s", len(files), time.Since(startTime).Round(time.Second)))
				statusLabel.SetText("Cập nhật hoàn tất!")

				refreshBackups()
				launchIPCAS()
				showMsg("Hoàn tất", fmt.Sprintf("Đã cập nhật %d file", len(files)))
			}

			// Show dialog with 3 options
			showUpdateConfirm(
				"Cập nhật IPCAS2",
				fmt.Sprintf("Có %d file cần cập nhật.\nBạn muốn backup trước không?\n(Giới hạn 3 bản backup)", len(files)),
				func() {
					// Option 1: Backup then Update
					addLog("Đang tạo backup trước khi cập nhật...")
					statusLabel.SetText("Đang backup...")
					if err := createBackup(); err != nil {
						addLog("Lỗi backup: " + err.Error())
					} else {
						addLog("Backup hoàn tất")
						refreshBackups()
					}
					performFilesUpdate()
				},
				func() {
					// Option 2: Update without backup
					addLog("Cập nhật không backup theo yêu cầu người dùng")
					performFilesUpdate()
				},
			)
		}() // Close goroutine
	}

	// Restore from backup
	doRestore := func() {
		if backupList.Selected == "" {
			showMsg("Lỗi", "Vui lòng chọn bản backup")
			return
		}

		backupPath := filepath.Join(updateBackupDir, backupList.Selected)
		addLog("Đang restore từ: " + backupList.Selected)

		killIPCAS()

		// Open zip
		r, err := zip.OpenReader(backupPath)
		if err != nil {
			addLog("Lỗi mở backup: " + err.Error())
			return
		}
		defer r.Close()

		progressBar.Show()
		total := len(r.File)

		for i, f := range r.File {
			dstPath := filepath.Join(updateTargetPath, f.Name)
			os.MkdirAll(filepath.Dir(dstPath), 0755)

			rc, err := f.Open()
			if err != nil {
				addLog("Lỗi mở file: " + f.Name)
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				addLog("Lỗi đọc file: " + f.Name)
				continue
			}

			os.WriteFile(dstPath, data, 0644)
			addLog("Restore: " + f.Name)

			progressBar.SetValue(float64(i+1) / float64(total))
		}

		progressBar.Hide()
		addLog("Restore hoàn tất!")
		statusLabel.SetText("Restore hoàn tất!")

		launchIPCAS()
		showMsg("Hoàn tất", "Đã restore từ "+backupList.Selected)
	}

	// Check only (no update) - runs in background
	doCheck := func() {
		saveConfig()
		statusLabel.SetText("Đang kiểm tra...")
		progressBar.Show()
		progressBar.SetValue(0)

		go func() {
			files, err := getFilesToUpdate()

			// Update UI from main thread context
			progressBar.Hide()

			if err != nil {
				addLog("Lỗi: " + err.Error())
				statusLabel.SetText("Lỗi kết nối")
				return
			}

			if len(files) == 0 {
				addLog("Không có file cần cập nhật")
				statusLabel.SetText("✅ Đã cập nhật mới nhất")
			} else {
				addLog(fmt.Sprintf("Có %d file cần cập nhật:", len(files)))
				// Only log first 10 files to avoid spam
				for i, f := range files {
					if i >= 10 {
						addLog(fmt.Sprintf("  ... và %d file khác", len(files)-10))
						break
					}
					addLog("  - " + f)
				}
				statusLabel.SetText(fmt.Sprintf("⚠️ Có %d file cần cập nhật", len(files)))
			}
		}()
	}

	// Initial refresh
	go func() {
		time.Sleep(300 * time.Millisecond)
		refreshBackups()
	}()

	return container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Cập nhật IPCAS2"),
			widget.NewLabel("Đường dẫn nguồn:"),
			sourceEntry,
			container.NewGridWithColumns(3,
				widget.NewButton("Kiểm tra", doCheck),
				widget.NewButton("Cập nhật", doUpdate),
				widget.NewButton("Lưu cấu hình", saveConfig),
			),
			progressBar,
			statusLabel,
			widget.NewSeparator(),
			widget.NewLabel("Backup & Restore:"),
			backupList,
			container.NewGridWithColumns(2,
				widget.NewButton("Tạo Backup", func() {
					if err := createBackup(); err != nil {
						showMsg("Lỗi", err.Error())
					} else {
						refreshBackups()
						showMsg("Thành công", "Đã tạo backup")
					}
				}),
				widget.NewButton("Restore", doRestore),
			),
			widget.NewSeparator(),
			widget.NewLabel("Log:"),
		),
		nil, nil, nil,
		container.NewScroll(logText),
	)
}
