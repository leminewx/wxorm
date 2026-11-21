package wxorm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"text/template"
)

// LogoStyle 指定 ShowLogo 绘制哪一种启动标识。
type LogoStyle int32

const (
	// LOGO_STYLE_CLASSIC 为宽幅艺术字，也是默认样式。
	LOGO_STYLE_CLASSIC LogoStyle = iota
	// LOGO_STYLE_PIXEL 为实心像素块，视觉冲击最强。
	LOGO_STYLE_PIXEL
	// LOGO_STYLE_SHADOW 为双线阴影，依赖终端 Unicode 制表符。
	LOGO_STYLE_SHADOW
	// LOGO_STYLE_ASCII 为纯 ASCII 井号体，任何编码与字体下都不会错位。
	LOGO_STYLE_ASCII
)

// currentLogoStyle 保存当前标识样式。
//
// 用原子整数而不是普通变量：样式可能在启动协程中设置，
// 而 ShowLogo 可能被并发调用（多入口进程或并行测试）。
var currentLogoStyle atomic.Int32

// SetLogoStyle 设置 ShowLogo 使用的标识样式，越界值被忽略。
func SetLogoStyle(style LogoStyle) {
	if style < LOGO_STYLE_CLASSIC || style > LOGO_STYLE_ASCII {
		return
	}

	currentLogoStyle.Store(int32(style))
}

// GetLogoStyle 返回当前标识样式。
func GetLogoStyle() LogoStyle {
	return LogoStyle(currentLogoStyle.Load())
}

const (
	// logoArtClassic 为宽幅艺术字。
	logoArtClassic = `
__        __ __  __   ___    ____    __  __
\ \      / / \ \/ /  / _ \  |  _ \  |  \/  |
 \ \ /\ / /   \  /  | | | | | |_) | | \  / |
  \ V  V /    /  \  | |_| | |  _ <  | |\/| |
   \_/\_/    /_/\_\  \___/  |_| \_\ |_|  |_|
`

	// logoArtPixel 为实心像素块，41 列。
	logoArtPixel = `
█   █ █   █  ███  ████  █   █
█   █  █ █  █   █ █   █ ██ ██
█ █ █   █   █   █ ████  █ █ █
██ ██  █ █  █   █ █  █  █   █
█   █ █   █  ███  █   █ █   █
`

	// logoArtShadow 为双线阴影，需要终端支持 Unicode 制表符。
	logoArtShadow = `
██╗    ██╗ ██╗  ██╗  ██████╗  ██████╗  ███╗   ███╗
██║    ██║ ╚██╗██╔╝ ██╔═══██╗ ██╔══██╗ ████╗ ████║
██║ █╗ ██║  ╚███╔╝  ██║   ██║ ██████╔╝ ██╔████╔██║
██║███╗██║  ██╔██╗  ██║   ██║ ██╔══██╗ ██║╚██╔╝██║
╚███╔███╔╝ ██╔╝ ██╗ ╚██████╔╝ ██║  ██║ ██║ ╚═╝ ██║
 ╚══╝╚══╝  ╚═╝  ╚═╝  ╚═════╝  ╚═╝  ╚═╝ ╚═╝     ╚═╝
`

	// logoArtASCII 为纯 ASCII 井号体，不依赖任何扩展字符。
	logoArtASCII = `
#   # #   #  ###  ####  #   #
#   #  # #  #   # #   # ## ##
# # #   #   #   # ####  # # #
## ##  # #  #   # #  #  #   #
#   # #   #  ###  #   # #   #
`

	// logoBody 为各样式共用的应用名、版本与发布信息。
	logoBody = `
   Copyright (c) {{.Year}} Leminewx. All rights reserved.
---------------------------------------------------------------
   AppName: {{.AppName}}
   Version: {{.Version}}
   Release: {{.Release}}
---------------------------------------------------------------
`
)

// logoData 是标识模板渲染所需的元信息。
type logoData struct {
	AppName string
	Year    string
	Version string
	Release string
}

// getLogoTemplate 返回样式对应的模板，未知样式回落到默认样式。
func getLogoTemplate(style LogoStyle) string {
	switch style {
	case LOGO_STYLE_PIXEL:
		return logoArtPixel + logoBody
	case LOGO_STYLE_SHADOW:
		return logoArtShadow + logoBody
	case LOGO_STYLE_ASCII:
		return logoArtASCII + logoBody
	default:
		return logoArtClassic + logoBody
	}
}

/*
WriteLogo 按当前样式把启动标识写入 writer

需要接管输出（例如写入启动日志文件）时使用；
样式是进程级设置，可先用 SetLogoStyle 切换。
*/
func WriteLogo(writer io.Writer) error {
	if writer == nil {
		return errors.New("wxorm: logo writer is nil")
	}

	tmpl, err := template.New("logo").Parse(getLogoTemplate(GetLogoStyle()))
	if err != nil {
		return fmt.Errorf("wxorm: failed to parse logo template: %w", err)
	}

	if err = tmpl.Execute(writer, &logoData{
		AppName: "WxORM",
		Year:    year,
		Version: version,
		Release: release,
	}); err != nil {
		return fmt.Errorf("wxorm: failed to write logo: %w", err)
	}

	return nil
}

// ShowLogo 把当前样式的启动标识写到标准输出。
func ShowLogo() error {
	return WriteLogo(os.Stdout)
}
