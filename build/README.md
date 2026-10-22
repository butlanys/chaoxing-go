# 构建说明

## Linux (本机 arm64 / amd64)

Debian 13 (trixie) 只提供 webkit2gtk-4.1，因此**必须**带上 `webkit2_41` 构建标签：

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
wails build -tags webkit2_41
```

产物: `build/bin/chaoxing-go`

> 注意标签名是 `webkit2_41`（不是 `webkit2gtk_4_1`），见 wails 源码
> `internal/frontend/desktop/linux/*.go` 的 `#cgo webkit2_41 pkg-config: webkit2gtk-4.1 libsoup-3.0`。

## Windows (交叉编译)

```bash
sudo apt install gcc-mingw-w64-x86-64 nsis
wails build -platform windows/amd64 -nsis
```

## 命令行版（无 GUI）

```bash
go build -o chaoxing ./cmd/chaoxing
./chaoxing -c config.ini
```

## 仅跑测试

```bash
go test ./...
```
