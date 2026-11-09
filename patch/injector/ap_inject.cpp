// APInjector - starts the game and injects the patch DLL once the engine is
// up (attach injection), or injects into an already running one.
//
// Usage:
//   ap_inject.exe <path\to\AstralParty.exe> [game args...]
//   ap_inject.exe --pid <processId>
//   ap_inject.exe --dll <path\to\ap_patch.dll> ... (override default dll next to this exe)
//   ap_inject.exe --steam-stub ... (copy the steam_api64 stub next to the game exe)

#include <windows.h>
#include <tlhelp32.h>
#include <cstdio>
#include <cstring>
#include <string>

static std::wstring ExeDir() {
    wchar_t path[MAX_PATH] = {};
    GetModuleFileNameW(nullptr, path, MAX_PATH);
    std::wstring dir(path);
    size_t slash = dir.find_last_of(L"\\/");
    return slash == std::wstring::npos ? L"." : dir.substr(0, slash);
}

static bool CopySteamStub(const std::wstring& gameDir) {
    std::wstring src = ExeDir() + L"\\steam_api64.dll";
    std::wstring dst = gameDir + L"\\steam_api64.dll";
    if (!CopyFileW(src.c_str(), dst.c_str(), FALSE)) {
        wprintf(L"copying %ls -> %ls failed (%lu)\n", src.c_str(), dst.c_str(), GetLastError());
        return false;
    }
    wprintf(L"steam_api64 stub deployed to game dir\n");
    return true;
}

static bool InjectInto(HANDLE process, const std::wstring& dllPath) {
    const size_t bytes = (dllPath.size() + 1) * sizeof(wchar_t);
    LPVOID remote = VirtualAllocEx(process, nullptr, bytes, MEM_COMMIT | MEM_RESERVE,
                                   PAGE_READWRITE);
    if (!remote) {
        wprintf(L"VirtualAllocEx failed (%lu)\n", GetLastError());
        return false;
    }
    if (!WriteProcessMemory(process, remote, dllPath.c_str(), bytes, nullptr)) {
        wprintf(L"WriteProcessMemory failed (%lu)\n", GetLastError());
        return false;
    }
    HMODULE kernel32 = GetModuleHandleW(L"kernel32.dll");
    LPTHREAD_START_ROUTINE loadLibrary = reinterpret_cast<LPTHREAD_START_ROUTINE>(
        GetProcAddress(kernel32, "LoadLibraryW"));
    if (!loadLibrary) {
        wprintf(L"LoadLibraryW not found\n");
        return false;
    }
    HANDLE thread = CreateRemoteThread(process, nullptr, 0, loadLibrary, remote, 0, nullptr);
    if (!thread) {
        wprintf(L"CreateRemoteThread failed (%lu)\n", GetLastError());
        return false;
    }
    WaitForSingleObject(thread, 10000);
    CloseHandle(thread);
    VirtualFreeEx(process, remote, 0, MEM_RELEASE);
    return true;
}

static bool InjectByName(DWORD pid, const std::wstring& dllPath) {
    HANDLE process = OpenProcess(PROCESS_ALL_ACCESS, FALSE, pid);
    if (!process) {
        wprintf(L"OpenProcess(%lu) failed (%lu)\n", pid, GetLastError());
        return false;
    }
    bool ok = InjectInto(process, dllPath);
    CloseHandle(process);
    if (ok) wprintf(L"injected into pid %lu\n", pid);
    return ok;
}

static bool FindGamePidByName(DWORD* outPid) {
    HANDLE snap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (snap == INVALID_HANDLE_VALUE) return false;
    PROCESSENTRY32W pe = {};
    pe.dwSize = sizeof(pe);
    bool found = false;
    if (Process32FirstW(snap, &pe)) {
        do {
            if (_wcsicmp(pe.szExeFile, L"AstralParty_INT.exe") == 0) {
                *outPid = pe.th32ProcessID;
                found = true;
                break;
            }
        } while (Process32NextW(snap, &pe));
    }
    CloseHandle(snap);
    return found;
}

static bool ModuleLoaded(DWORD pid, const wchar_t* name) {
    HANDLE snap = CreateToolhelp32Snapshot(TH32CS_SNAPMODULE, pid);
    if (snap == INVALID_HANDLE_VALUE) return false;
    MODULEENTRY32W me = {};
    me.dwSize = sizeof(me);
    bool found = false;
    if (Module32FirstW(snap, &me)) {
        do {
            if (_wcsicmp(me.szModule, name) == 0) {
                found = true;
                break;
            }
        } while (Module32NextW(snap, &me));
    }
    CloseHandle(snap);
    return found;
}

static bool GameWindowUp(DWORD pid) {
    struct Ctx { DWORD pid; HWND hwnd; };
    Ctx ctx = { pid, nullptr };
    EnumWindows([](HWND hwnd, LPARAM lp) -> BOOL {
        Ctx* c = (Ctx*)lp;
        DWORD pid = 0;
        GetWindowThreadProcessId(hwnd, &pid);
        if (pid == c->pid && IsWindowVisible(hwnd)) {
            wchar_t cls[64] = {};
            GetClassNameW(hwnd, cls, 64);
            if (_wcsicmp(cls, L"UnityWndClass") == 0) {
                c->hwnd = hwnd;
                return FALSE;
            }
        }
        return TRUE;
    }, (LPARAM)&ctx);
    return ctx.hwnd != nullptr;
}

int wmain(int argc, wchar_t** argv) {
    std::wstring dll = ExeDir() + L"\\ap_patch.dll";
    DWORD pid = 0;
    bool steamStub = false;

    for (int i = 1; i < argc; i++) {
        if (wcscmp(argv[i], L"--dll") == 0 && i + 1 < argc) {
            dll = argv[++i];
        } else if (wcscmp(argv[i], L"--pid") == 0 && i + 1 < argc) {
            pid = (DWORD)_wtoi(argv[++i]);
        } else if (wcscmp(argv[i], L"--steam-stub") == 0) {
            steamStub = true;
        }
    }

    if (pid) {
        return InjectByName(pid, dll) ? 0 : 1;
    }
    if (argc < 2 || (argc > 1 && wcscmp(argv[1], L"--help") == 0)) {
        wprintf(L"usage:\n  ap_inject.exe <game.exe> [args...]   start + inject\n"
                L"  ap_inject.exe --pid <pid>             inject into running game\n"
                L"  ap_inject.exe --dll <path> ...        custom dll (default: ap_patch.dll beside this exe)\n"
                L"  ap_inject.exe --steam-stub ...        also copy the steam_api64 stub to the game dir\n");
        return 2;
    }

    std::wstring exe = argv[1];
    // Resolve to an absolute path: relative exe names would otherwise break
    // the workdir derivation below (CreateProcess error 267).
    wchar_t fullPath[MAX_PATH] = {};
    if (GetFullPathNameW(exe.c_str(), MAX_PATH, fullPath, nullptr) && fullPath[0]) {
        exe = fullPath;
    }
    std::wstring cmd = L"\"" + exe + L"\"";
    for (int i = 2; i < argc; i++) {
        cmd += L" \"";
        cmd += argv[i];
        cmd += L"\"";
    }
    std::wstring workdir = exe;
    size_t slash = workdir.find_last_of(L"\\/");
    if (slash != std::wstring::npos) workdir.resize(slash);
    if (workdir.empty() || workdir.find(L"\\") == std::wstring::npos) {
        wchar_t cwd[MAX_PATH] = {};
        GetCurrentDirectoryW(MAX_PATH, cwd);
        workdir = cwd;
    }

    if (steamStub) {
        CopySteamStub(workdir);
    }

    STARTUPINFOW si = {};
    si.cb = sizeof(si);
    PROCESS_INFORMATION pi = {};
    // Normal start, no CREATE_SUSPENDED. Injecting at process birth races the
    // engine's early init (CRIWARE / Unity loader) and crashes the game
    // before the hot-update DLL even loads. The patch DLL itself waits for
    // il2cpp metadata stability, so attaching once the engine is up is safe
    // and still lands before login-time hooks are needed.
    if (!CreateProcessW(nullptr, cmd.data(), nullptr, nullptr, FALSE,
                        0, nullptr, workdir.c_str(), &si, &pi)) {
        wprintf(L"CreateProcess failed (%lu)\n", GetLastError());
        return 1;
    }
    wprintf(L"game started (pid %lu), waiting for engine init...\n", pi.dwProcessId);
    bool ready = false;
    for (int i = 0; i < 3000; i++) { // up to 5 minutes
        if (WaitForSingleObject(pi.hProcess, 0) == WAIT_OBJECT_0) {
            wprintf(L"game exited before injection point\n");
            CloseHandle(pi.hThread);
            CloseHandle(pi.hProcess);
            return 1;
        }
        if (ModuleLoaded(pi.dwProcessId, L"GameAssembly.dll") && GameWindowUp(pi.dwProcessId)) {
            ready = true;
            break;
        }
        Sleep(100);
    }
    if (!ready) {
        wprintf(L"engine never reached the injection point (GameAssembly + window)\n");
        CloseHandle(pi.hThread);
        CloseHandle(pi.hProcess);
        return 1;
    }
    Sleep(3000); // let plugin init (CRIWARE etc.) settle
    wprintf(L"engine ready, injecting %ls\n", dll.c_str());
    bool ok = InjectInto(pi.hProcess, dll);
    if (!ok) {
        wprintf(L"injection failed, game keeps running unpatched\n");
    }
    CloseHandle(pi.hThread);
    CloseHandle(pi.hProcess);
    return ok ? 0 : 1;
}
