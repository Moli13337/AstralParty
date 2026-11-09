@echo off
rem Builds ap_patch.dll and ap_inject.exe into patch\out\.
setlocal
cd /d "%~dp0"

set VSWHERE="C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe"
set VSPATH=
if exist %VSWHERE% (
    for /f "usebackq delims=" %%i in (`%VSWHERE% -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`) do set VSPATH=%%i
)
set VCVARS="%VSPATH%\VC\Auxiliary\Build\vcvars64.bat"

if exist %VCVARS% (
    echo [build] using MSVC at %VSPATH%
    call %VCVARS% >nul
    if not exist out mkdir out
    cl /nologo /W3 /O2 /EHsc /std:c++17 /D_CRT_SECURE_NO_WARNINGS /LD /DUNICODE /D_UNICODE ^
       dll\ap_patch.cpp dll\il2cpp_api.cpp /Fe:out\ap_patch.dll /Fo:out\ || exit /b 1
    cl /nologo /W3 /O2 /EHsc /std:c++17 /D_CRT_SECURE_NO_WARNINGS /DUNICODE /D_UNICODE ^
       injector\ap_inject.cpp /Fe:out\ap_inject.exe /Fo:out\ /link user32.lib || exit /b 1
    cl /nologo /W3 /O2 /EHsc /std:c++17 /D_CRT_SECURE_NO_WARNINGS /LD ^
       stub\steam_api64.cpp /Fe:out\steam_api64.dll /Fo:out\ || exit /b 1
) else (
    where g++ >nul 2>nul || (echo [build] no MSVC or g++ found & exit /b 1)
    echo [build] using g++
    if not exist out mkdir out
    g++ -O2 -std=c++17 -shared -static -static-libgcc -static-libstdc++ ^
        -o out\ap_patch.dll dll\ap_patch.cpp dll\il2cpp_api.cpp || exit /b 1
    g++ -O2 -std=c++17 -static -static-libgcc -static-libstdc++ -municode ^
        -o out\ap_inject.exe injector\ap_inject.cpp -luser32 || exit /b 1
    g++ -O2 -std=c++17 -shared -static -static-libgcc -static-libstdc++ ^
        -o out\steam_api64.dll stub\steam_api64.cpp || exit /b 1
)

copy /y config\ap_patch.json out\ap_patch.json >nul
echo [build] output: patch\out\{ap_patch.dll, ap_inject.exe, steam_api64.dll, ap_patch.json}
