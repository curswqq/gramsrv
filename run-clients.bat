@echo off
setlocal EnableDelayedExpansion

REM ===== Путь к Telegram.exe =====
set TELEGRAM=E:\projects\tdesktop\out\Debug\Telegram.exe

if not exist "%TELEGRAM%" (
    echo.
    echo Telegram.exe not found:
    echo %TELEGRAM%
    pause
    exit /b 1
)

echo.
set /p COUNT=How many clients to start? 

echo.

for /L %%i in (1,1,%COUNT%) do (
    set /p NAME=Client %%i name: 

    if not exist ".tdata-!NAME!" (
        mkdir ".tdata-!NAME!"
    )

    echo Starting !NAME!...
    start "" "%TELEGRAM%" -workdir "%CD%\.tdata-!NAME!"

    timeout /t 1 >nul
)

echo.
echo ======================================
echo All clients started.
echo Login code: 12345
echo ======================================
pause