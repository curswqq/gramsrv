@echo off
setlocal

cd /d "%~dp0"

echo ========================================
echo Building telesrv...
echo ========================================
go build -v -o bin\telesrv.exe ./cmd/telesrv
if errorlevel 1 goto :error

echo.
echo ========================================
echo Building telesrv-admin...
echo ========================================
go build -v -o bin\telesrv-admin.exe ./cmd/telesrv-admin
if errorlevel 1 goto :error

echo.
echo ========================================
echo Copying executables...
echo ========================================

copy /Y "bin\telesrv.exe" "telesrv.exe" >nul
if errorlevel 1 goto :error

copy /Y "bin\telesrv-admin.exe" "telesrv-admin.exe" >nul
if errorlevel 1 goto :error

echo.
echo ========================================
echo Build completed successfully!
echo ========================================
echo.
echo Created:
echo   telesrv.exe
echo   telesrv-admin.exe
echo.
pause
exit /b 0

:error
echo.
echo ========================================
echo BUILD FAILED!
echo ========================================
pause
exit /b 1