@echo off

echo Usage: .\win-build.bat [--target=^<target^>]
echo Options:
echo   --target=^<target^>  Build target: amd64, arm64, appx-amd64, appx-arm64, or all (default: all)
echo.

REM 保存初始目录
set "INITIAL_DIR=%cd%"

REM 获取脚本所在目录
set "SCRIPT_DIR=%~dp0"
REM 移除末尾的反斜杠
if "%SCRIPT_DIR:~-1%"=="\" set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"

REM 获取项目根目录（脚本目录的父目录）
for %%I in ("%SCRIPT_DIR%\..") do set "PROJECT_ROOT=%%~fI"

set "TARGET=all"

:parse_args
if "%1"=="" goto :end_parse_args
if "%1"=="--target" (
    if "%2"=="" (
        echo Error: --target option requires an equal sign and value
        echo Usage: --target=^<target^>
        echo Example: --target=amd64
        exit /b 1
    ) else (
        if not "%2"=="amd64" if not "%2"=="arm64" if not "%2"=="appx-amd64" if not "%2"=="appx-arm64" if not "%2"=="all" (
            echo Error: Invalid target '%2'
            echo Valid targets are: amd64, arm64, appx-amd64, appx-arm64, all
            exit /b 1
        )
        set "TARGET=%2"
        shift
        shift
        goto :parse_args
    )
) else (
    @REM Skip unknown options
    shift
    goto :parse_args
)
:end_parse_args

if "%TARGET%"=="amd64" (
    set BUILD_AMD64=1
) else if "%TARGET%"=="arm64" (
    set BUILD_ARM64=1
) else if "%TARGET%"=="appx-amd64" (
    set BUILD_APPX_AMD64=1
) else if "%TARGET%"=="appx-arm64" (
    set BUILD_APPX_ARM64=1
) else (
    REM all: build everything
    set BUILD_AMD64=1
    set BUILD_ARM64=1
    set BUILD_APPX_AMD64=1
    set BUILD_APPX_ARM64=1
)

echo Cleaning Builds
rmdir /S /Q "%PROJECT_ROOT%\app\build" 1>nul
rmdir /S /Q "%PROJECT_ROOT%\app\kernel" 1>nul
rmdir /S /Q "%PROJECT_ROOT%\app\kernel-arm64" 1>nul

echo.
echo Building UI
cd app
SET ELECTRON_MIRROR=https://npmmirror.com/mirrors/electron/
if errorlevel 1 (
    exit /b %errorlevel%
)
call pnpm install
if errorlevel 1 (
    exit /b %errorlevel%
)
call pnpm run build
if errorlevel 1 (
    exit /b %errorlevel%
)

echo.
echo Building Kernel
@REM the C compiler "gcc" is necessary https://sourceforge.net/projects/mingw-w64/files/mingw-w64/
cd /d "%PROJECT_ROOT%\kernel"
if errorlevel 1 (
    exit /b %errorlevel%
)

echo 'Cleaning Builds'
del /S /Q /F app\build 1>nul
del /S /Q /F app\kernel 1>nul


echo Building Kernel
@REM the C compiler "gcc" is necessary https://sourceforge.net/projects/mingw-w64/files/mingw-w64/
go version
set GO111MODULE=on
set GOPROXY=https://mirrors.aliyun.com/goproxy/
set CGO_ENABLED=1
set GOOS=windows

@REM you can use `go mod tidy` to update kernel dependency before build
@REM you can use `go generate` instead (need add something in main.go)
goversioninfo -platform-specific=true -icon=resource/icon.ico -manifest=resource/goversioninfo.exe.manifest

if defined BUILD_AMD64 (
    echo.
    echo Building Kernel amd64
    set GOARCH=amd64
    go build --tags fts5 -v -o "%PROJECT_ROOT%\app\kernel\SiYuan-Kernel.exe" -ldflags "-s -w -H=windowsgui" .
    if errorlevel 1 (
        exit /b %errorlevel%
    )
)


cd ..
if errorlevel 1 (
    exit /b %errorlevel%
)

cd app
call pnpm run dist
if errorlevel 1 (
    exit /b %errorlevel%
)

cd ..