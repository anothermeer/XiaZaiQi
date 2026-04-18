Originally it's just chinese "下载器" but I really don't know that to put for name...

## Building Requirements
- Windows 10/11
- Go > v1.26.2
- Fyne >= v1.7.0
- Fyne Library >= v2.7.3

## How To Build
1. Clone the repository
2. `cd XiaZaiQi`
3. `go mod tidy`
4. `go build -ldflags "-H=windowsgui" -o "wnxzq.exe" main.go`
5. done
