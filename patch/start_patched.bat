@echo off
rem Launch Astral Party with the private-server patch injected.
cd /d "%~dp0"
if not exist ap_inject.exe (
    echo ap_inject.exe not found next to this script.
    pause
    exit /b 1
)
ap_inject.exe AstralParty_INT.exe
echo.
echo Game exited or injector finished. Check ap_patch.log for details.
pause
