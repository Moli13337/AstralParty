// Astral Party client patch.
//
// Redirects the game to a private server and removes the Steam SDK login
// dependency. Verified hook targets (all AOT, resolved by name):
//   MaxContextPlatformSteam::Send - SDK event dispatch (vtable-dispatched, so
//     it is inline-hooked). "loginSteam"/"loginOAuth" normally fetch a Steam
//     auth ticket via Facepunch.Steamworks; the hook answers with a synthetic
//     ticket and never touches Steam.
//   SteamManager::Awake     - skipped so Steamworks never initializes;
//     m_bInitialized is forced true so MaxContextPlatformSteam::OnCreate passes.
//   UnityEngine.Debug.Log/LogWarning/LogError - mirrored into the patch
//     console window so hot-update (GameLogic) developer logs are visible.
//
// The login success callback shape was taken from
// MaxContextPlatformSteam.<Login>d__6::MoveNext:
//   dict["ticket"] = <hex ticket>  ->  MaxLibUnity.Receive("", idx, 1, json)
// The empty event name is the same literal the real success path uses, and
// "getCurrentLanguage" answers {"language":..,"steam_id":..} the same way.

#include "il2cpp_api.h"

#include <cstdarg>
#include <cstdio>
#include <cstring>
#include <io.h>
#include <string>
#include <vector>

// ---------------------------------------------------------------- logging

static void Log(const char* fmt, ...);

// Converts UTF-16 to UTF-8.
static std::string Utf8(const std::wstring& w);

// Creates every missing directory component of the path.
static void CreateDirsRecursive(const std::wstring& dir);

// ----------------------------------------------------------------- config

struct PatchConfig {
    std::string server_ip = "192.168.1.100";
    int server_port = 8800;
    std::string log_ip = "127.0.0.1:9999";
    std::string notice_ip = "127.0.0.1";
    std::string cdn_url = "http://127.0.0.1/";
    std::string steam_id = "76561198000000001";
    std::string language = "english";
    bool gm = true;
    bool recharge_test = true;
    bool write_hacker_config = true;
    bool hook_login = true;
    bool console = true;
    bool hook_debug_log = true;
    int log_level = 1;
};

static PatchConfig g_cfg;

static std::string Trim(const std::string& s) {
    size_t a = s.find_first_not_of(" \t\r\n");
    if (a == std::string::npos) return "";
    size_t b = s.find_last_not_of(" \t\r\n");
    return s.substr(a, b - a + 1);
}

// Parses a flat JSON object of string/number/bool values. Enough for the
// patch config; deliberately dependency-free.
static bool ParseFlatJson(const std::string& text, PatchConfig& cfg) {
    size_t pos = 0;
    auto skipWs = [&]() {
        while (pos < text.size() && isspace(static_cast<unsigned char>(text[pos]))) pos++;
    };
    auto readString = [&](std::string& out) {
        if (pos >= text.size() || text[pos] != '"') return false;
        pos++;
        out.clear();
        while (pos < text.size() && text[pos] != '"') {
            if (text[pos] == '\\' && pos + 1 < text.size()) {
                pos++;
                char c = text[pos++];
                switch (c) {
                    case 'n': out += '\n'; break;
                    case 't': out += '\t'; break;
                    case 'r': out += '\r'; break;
                    case '"': out += '"'; break;
                    case '\\': out += '\\'; break;
                    case '/': out += '/'; break;
                    default: out += c; break;
                }
            } else {
                out += text[pos++];
            }
        }
        if (pos < text.size()) pos++; // closing quote
        return true;
    };
    auto readValue = [&](std::string& out) {
        if (pos < text.size() && text[pos] == '"') return readString(out);
        size_t start = pos;
        while (pos < text.size() && text[pos] != ',' && text[pos] != '}') pos++;
        out = Trim(text.substr(start, pos - start));
        return true;
    };

    skipWs();
    if (pos >= text.size() || text[pos] != '{') return false;
    pos++;
    while (pos < text.size()) {
        skipWs();
        if (pos < text.size() && text[pos] == '}') break;
        std::string key;
        if (!readString(key)) return false;
        skipWs();
        if (pos >= text.size() || text[pos] != ':') return false;
        pos++;
        skipWs();
        std::string value;
        readValue(value);
        if (key == "server_ip") cfg.server_ip = value;
        else if (key == "server_port") cfg.server_port = atoi(value.c_str());
        else if (key == "log_ip") cfg.log_ip = value;
        else if (key == "notice_ip") cfg.notice_ip = value;
        else if (key == "cdn_url") cfg.cdn_url = value;
        else if (key == "steam_id") cfg.steam_id = value;
        else if (key == "language") cfg.language = value;
        else if (key == "gm") cfg.gm = value == "true";
        else if (key == "recharge_test") cfg.recharge_test = value == "true";
        else if (key == "write_hacker_config") cfg.write_hacker_config = value == "true";
        else if (key == "hook_login") cfg.hook_login = value == "true";
        else if (key == "console") cfg.console = value == "true";
        else if (key == "hook_debug_log") cfg.hook_debug_log = value == "true";
        else if (key == "log_level") cfg.log_level = atoi(value.c_str());
        skipWs();
        if (pos < text.size() && text[pos] == ',') pos++;
    }
    return true;
}

static std::string Utf8(const std::wstring& w) {
    if (w.empty()) return "";
    int n = WideCharToMultiByte(CP_UTF8, 0, w.c_str(), (int)w.size(), nullptr, 0, nullptr, nullptr);
    std::string out(static_cast<size_t>(n), '\0');
    WideCharToMultiByte(CP_UTF8, 0, w.c_str(), (int)w.size(), &out[0], n, nullptr, nullptr);
    return out;
}

static std::string ModuleDir(HMODULE mod) {
    wchar_t path[MAX_PATH] = {};
    GetModuleFileNameW(mod, path, MAX_PATH);
    std::wstring dir(path);
    size_t slash = dir.find_last_of(L"\\/");
    if (slash == std::wstring::npos) return "";
    return Utf8(dir.substr(0, slash));
}

static bool LoadConfig(HMODULE patchModule) {
    std::string path = ModuleDir(patchModule) + "\\ap_patch.json";
    HANDLE f = CreateFileA(path.c_str(), GENERIC_READ, FILE_SHARE_READ, nullptr,
                           OPEN_EXISTING, 0, nullptr);
    if (f == INVALID_HANDLE_VALUE) {
        Log("ap_patch.json not found at %s, using defaults", path.c_str());
        return false;
    }
    char buf[8192] = {};
    DWORD read = 0;
    ReadFile(f, buf, sizeof(buf) - 1, &read, nullptr);
    CloseHandle(f);
    bool ok = ParseFlatJson(buf, g_cfg);
    Log("config loaded from %s (ok=%d)", path.c_str(), ok ? 1 : 0);
    return ok;
}

// ------------------------------------------------ patch log console/file

// The patch allocates its own console window ("cmd") so both patch output
// and mirrored game Debug.Log output are visible live. Everything is also
// appended to ap_patch.log next to the DLL.
static FILE* g_console = nullptr;
static SRWLOCK g_logLock = SRWLOCK_INIT;

static void InitConsole() {
    if (!g_cfg.console) {
        return;
    }
    if (!AllocConsole()) {
        return; // process already has a console
    }
    SetConsoleTitleW(L"Astral Party Patch Log");
    FILE* fp = nullptr;
    freopen_s(&fp, "CONOUT$", "w", stdout);
    freopen_s(&fp, "CONOUT$", "w", stderr);
    HANDLE h = GetStdHandle(STD_OUTPUT_HANDLE);
    DWORD mode = 0;
    if (h && h != INVALID_HANDLE_VALUE && GetConsoleMode(h, &mode)) {
        SetConsoleMode(h, mode | ENABLE_VIRTUAL_TERMINAL_PROCESSING);
    }
    if (stdout) setvbuf(stdout, nullptr, _IONBF, 0);
    g_console = stdout;
    Log("console ready (pid %lu)", GetCurrentProcessId());
}

static void WriteLogLine(const char* level, const char* text) {
    SYSTEMTIME st;
    GetLocalTime(&st);

    AcquireSRWLockExclusive(&g_logLock);
    if (g_console) {
        fprintf(g_console, "\x1b[90m[%02d:%02d:%02d.%03d]\x1b[0m %s %s\n",
                st.wHour, st.wMinute, st.wSecond, st.wMilliseconds, level, text);
    }
    wchar_t dir[MAX_PATH] = {};
    HMODULE mod = nullptr;
    GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS |
                           GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
                       reinterpret_cast<LPCWSTR>(&g_cfg), &mod);
    GetModuleFileNameW(mod, dir, MAX_PATH);
    std::wstring path(dir);
    size_t slash = path.find_last_of(L"\\/");
    if (slash != std::wstring::npos) path.resize(slash);
    path += L"\\ap_patch.log";
    FILE* f = nullptr;
    _wfopen_s(&f, path.c_str(), L"a");
    if (f) {
        fprintf(f, "[%02d:%02d:%02d.%03d] %s %s\n",
                st.wHour, st.wMinute, st.wSecond, st.wMilliseconds, level, text);
        fclose(f);
    }
    ReleaseSRWLockExclusive(&g_logLock);
}

static void Log(const char* fmt, ...) {
    if (g_cfg.log_level <= 0) return;
    char text[2048];
    va_list args;
    va_start(args, fmt);
    vsnprintf_s(text, sizeof(text), _TRUNCATE, fmt, args);
    va_end(args);
    WriteLogLine("    ", text);
}

static void LogErr(const char* fmt, ...) {
    char text[2048];
    va_list args;
    va_start(args, fmt);
    vsnprintf_s(text, sizeof(text), _TRUNCATE, fmt, args);
    va_end(args);
    WriteLogLine("\x1b[31m[ERR]\x1b[0m", text);
}

static void LogGame(const char* level, const char* text) {
    char tag[32];
    snprintf(tag, sizeof(tag), "\x1b[%dm[game/%s]\x1b[0m",
             strcmp(level, "ERROR") == 0 ? 31 : strcmp(level, "WARN") == 0 ? 33 : 36,
             level);
    WriteLogLine(tag, text);
}

// ------------------------------------------------- HackerConfig JSON drop

// The game reads UELgkXDewH6YvC9R.json via HackerConfig.GetLocalFile from
// persistentDataPath or streamingAssetsPath before falling back to remote
// download. Writing it redirects every server address the client uses.
static void WriteHackerConfig() {
    std::string webServer = g_cfg.server_ip + ":" + std::to_string(g_cfg.server_port);
    char json[512];
    snprintf(json, sizeof(json),
             "{\"logServerIpPort\":\"%s\",\"webServerIpPort\":\"%s\",\"noticeIp\":\"%s\","
             "\"cdnUrl\":\"%s\",\"rechargeTest\":%s,\"GM\":%s}",
             g_cfg.log_ip.c_str(), webServer.c_str(), g_cfg.notice_ip.c_str(),
             g_cfg.cdn_url.c_str(), g_cfg.recharge_test ? "true" : "false",
             g_cfg.gm ? "true" : "false");

    wchar_t exePathW[MAX_PATH] = {};
    GetModuleFileNameW(nullptr, exePathW, MAX_PATH);
    std::wstring exePath(exePathW);
    size_t slash = exePath.find_last_of(L"\\/");
    if (slash == std::wstring::npos) return;
    std::wstring gameDir = exePath.substr(0, slash);           // ...\8vJXnINT
    std::wstring exeName = exePath.substr(slash + 1);          // AstralParty_INT.exe
    size_t dot = exeName.find_last_of(L'.');
    std::wstring stem = dot == std::wstring::npos ? exeName : exeName.substr(0, dot);
    std::wstring dataDir = gameDir + L"\\" + stem + L"_Data";  // ...\AstralParty_INT_Data

    std::vector<std::wstring> targets;

    // persistentDataPath = %USERPROFILE%\AppData\LocalLow\<company>\<product>
    // company/product come from <Game>_Data\app.info (two lines of text).
    std::wstring probe = dataDir + L"\\app.info";
    HANDLE f = CreateFileW(probe.c_str(), GENERIC_READ, FILE_SHARE_READ, nullptr,
                           OPEN_EXISTING, 0, nullptr);
    if (f != INVALID_HANDLE_VALUE) {
        char info[512] = {};
        DWORD read = 0;
        ReadFile(f, info, sizeof(info) - 1, &read, nullptr);
        CloseHandle(f);
        std::string text(info, read);
        size_t line1 = text.find('\n');
        std::string company = Trim(text.substr(0, line1));
        std::string product = Trim(text.substr(line1 + 1));
        wchar_t low[MAX_PATH] = {};
        ExpandEnvironmentStringsW(L"%USERPROFILE%\\AppData\\LocalLow", low, MAX_PATH);
        wchar_t persistent[MAX_PATH] = {};
        _snwprintf(persistent, MAX_PATH, L"%s\\%hs\\%hs", low, company.c_str(), product.c_str());
        targets.push_back(std::wstring(persistent) + L"\\UELgkXDewH6YvC9R.json");
    } else {
        Log("app.info not found at %ls, skipping persistent config drop", probe.c_str());
    }
    targets.push_back(dataDir + L"\\StreamingAssets\\UELgkXDewH6YvC9R.json");

    for (const std::wstring& target : targets) {
        std::wstring dir = target.substr(0, target.find_last_of(L"\\/"));
        CreateDirsRecursive(dir);
        HANDLE fh = CreateFileW(target.c_str(), GENERIC_WRITE, 0, nullptr,
                                CREATE_ALWAYS, 0, nullptr);
        if (fh == INVALID_HANDLE_VALUE) {
            Log("failed to write %ls", target.c_str());
            continue;
        }
        DWORD written = 0;
        WriteFile(fh, json, (DWORD)strlen(json), &written, nullptr);
        CloseHandle(fh);
        Log("hacker config written: %ls", target.c_str());
    }
}

static void CreateDirsRecursive(const std::wstring& dir) {
    std::wstring acc;
    for (size_t i = 0; i < dir.size(); i++) {
        acc += dir[i];
        if (dir[i] == L'\\' && i > 0) {
            CreateDirectoryW(acc.c_str(), nullptr);
        }
    }
    CreateDirectoryW(dir.c_str(), nullptr);
}

// Unity's Caching layer downloads AssetBundles into <LocalLow>\Unity\Temp
// with a single-level CreateDirectory that never builds missing parents. A
// dangling LocalLow\Unity symlink (left behind by an uninstalled game, e.g.
// NIKKE redirecting its cache to another drive) makes every bundle download
// fail with "Couldn't move cache data ''" and blocks the client at resource
// verification. Detect and repair that node before any download starts.
static void EnsureUnityCacheTemp() {
    wchar_t low[MAX_PATH] = {};
    ExpandEnvironmentStringsW(L"%USERPROFILE%\\AppData\\LocalLow\\Unity", low, MAX_PATH);
    std::wstring temp = std::wstring(low) + L"\\Temp";
    if (CreateDirectoryW(temp.c_str(), nullptr)) {
        Log("unity cache temp created: %ls", temp.c_str());
        return;
    }
    DWORD err = GetLastError();
    if (err == ERROR_ALREADY_EXISTS) return;
    if (err != ERROR_PATH_NOT_FOUND) {
        LogErr("unity cache temp create failed (err=%lu)", err);
        return;
    }
    DWORD attr = GetFileAttributesW(low);
    if (attr != INVALID_FILE_ATTRIBUTES && (attr & FILE_ATTRIBUTE_REPARSE_POINT)) {
        if (!RemoveDirectoryW(low)) {
            LogErr("dangling LocalLow\\Unity link removal failed (err=%lu)", GetLastError());
            return;
        }
        Log("removed dangling LocalLow\\Unity link");
    }
    if (!CreateDirectoryW(low, nullptr) && GetLastError() != ERROR_ALREADY_EXISTS) {
        LogErr("LocalLow\\Unity create failed (err=%lu)", GetLastError());
        return;
    }
    if (CreateDirectoryW(temp.c_str(), nullptr) || GetLastError() == ERROR_ALREADY_EXISTS) {
        Log("unity cache temp ready: %ls", temp.c_str());
    } else {
        LogErr("unity cache temp create failed after repair (err=%lu)", GetLastError());
    }
}

// ------------------------------------------------------------------ hooks

// MethodInfo.methodPointer is the first field of Il2CppMethodInfo on every
// supported Unity version; swapping it redirects all internal call sites
// without any inline patching.
static void* SwapMethodPointer(void* methodInfo, void* hook) {
    if (!methodInfo) return nullptr;
    void* orig = *static_cast<void**>(methodInfo);
    *static_cast<void**>(methodInfo) = hook;
    return orig;
}

// ------------------------------------------------------- inline trampolines

// Debug.Log & co. are called through statically-bound AOT code paths that do
// not consult MethodInfo.methodPointer, so pointer swaps never fire on them.
// A 14-byte inline detour (jmp [rip+0]; addr) covers every call form; the
// displaced prologue runs from a trampoline that jumps back.
//
// IL2CPP prologues contain rip-relative instructions (e.g.
// "cmp byte [rip+disp32], imm8"), so the displaced bytes must be length-decoded
// to a full instruction boundary and rip displacements re-based to the
// trampoline. Only instructions seen in il2cpp function prologues are
// supported; anything else refuses the hook (fail safe, no patch applied).

// Decodes one instruction. Returns its length or -1 when unsupported.
// ripDispOff receives the disp32 offset within the instruction for
// rip-relative memory operands (-1 when none).
static int DecodePrologueInsn(const uint8_t* p, int* ripDispOff) {
    *ripDispOff = -1;
    int i = 0;
    bool rexW = false;
    while (p[i] >= 0x40 && p[i] <= 0x4F) {
        rexW = (p[i] & 0x08) != 0;
        i++;
    }
    uint8_t op = p[i];
    if (op >= 0x50 && op <= 0x5F) return i + 1;            // push/pop r64
    if (op == 0x6A) return i + 2;                          // push imm8
    if (op == 0x68) return i + 5;                          // push imm32
    if (op == 0x90) return i + 1;                          // nop
    if (op >= 0xB8 && op <= 0xBF) return rexW ? i + 10 : i + 5; // mov reg, imm

    static const uint8_t modrmOps[] = {
        0x80, 0x81, 0x83, 0x88, 0x89, 0x8A, 0x8B, 0x30, 0x31,
        0x32, 0x33, 0x38, 0x39, 0x3A, 0x3B, 0x84, 0x85, 0x8D, 0x63,
    };
    bool hasModrm = false;
    for (uint8_t o : modrmOps) {
        if (op == o) { hasModrm = true; break; }
    }
    if (!hasModrm) return -1;

    uint8_t modrm = p[i + 1];
    uint8_t mod = modrm >> 6;
    uint8_t rm = modrm & 7;
    int len = i + 2; // opcode + modrm
    if (mod == 3) {
        // register operand: no SIB, no displacement
    } else if (mod == 0) {
        if (rm == 5) {
            if (op != 0x8D) *ripDispOff = len; // rip-relative disp32
            len += 4;
        } else if (rm == 4) {
            uint8_t sib = p[len];
            len++;
            if ((sib & 7) == 5) len += 4; // disp32 base
        }
    } else {
        if (rm == 4) len++; // SIB byte also present with mod=1/2
        len += (mod == 1) ? 1 : 4;
    }
    if (op == 0x80) len += 1;      // imm8
    else if (op == 0x81) len += 4; // imm32
    else if (op == 0x83) len += 1; // imm8
    else if (op >= 0xB8 && op <= 0xBF) { /* handled above */ }
    return len;
}

// Rip-relative displacements are int32, so a rebased instruction in the relay
// only reaches its original target when the relay sits within +-2GB of the
// hooked function. VirtualAlloc(nullptr) hands out arbitrary low addresses
// (0x1b3...), ~140TB away from GameAssembly (0x7ffc...); the cast truncated
// the shift and the trampoline faulted on its first rebased read. Scan
// outwards from the target in allocation-granularity steps instead.
static uint8_t* AllocNear(void* target, size_t size) {
    SYSTEM_INFO si;
    GetSystemInfo(&si);
    const uintptr_t step = si.dwAllocationGranularity; // 64KB
    const uintptr_t limit = 0x78000000;                // stay under 2GB
    const uintptr_t lo = (uintptr_t)si.lpMinimumApplicationAddress;
    const uintptr_t hi = (uintptr_t)si.lpMaximumApplicationAddress;
    const uintptr_t center = (uintptr_t)target & ~(step - 1);

    for (uintptr_t delta = 0; delta <= limit; delta += step) {
        for (int dir = 0; dir < 2; dir++) { // after target first, then before
            if (delta == 0 && dir == 1) continue;
            uintptr_t addr = dir == 0 ? center + delta : center - delta;
            if (addr < lo || addr > hi || addr == center) continue;
            void* p = VirtualAlloc((void*)addr, size, MEM_COMMIT | MEM_RESERVE,
                                   PAGE_EXECUTE_READWRITE);
            if (p) return static_cast<uint8_t*>(p);
        }
    }
    return nullptr;
}

static bool InlineHookPrologue(void* target, void* hookFn, void** trampolineOut) {
    uint8_t* fn = static_cast<uint8_t*>(target);

    // Decode until we have a full-instruction boundary of >= 14 bytes.
    struct RipRef { int off; int end; };
    RipRef rips[8];
    int ripCount = 0;
    int n = 0;
    while (n < 14) {
        int ripOff = -1;
        int len = DecodePrologueInsn(fn + n, &ripOff);
        if (len <= 0 || len > 15) {
            char dump[96];
            int pos = 0;
            for (int i = 0; i < 20 && pos < 88; i++) pos += snprintf(dump + pos, 96 - pos, "%02X ", fn[i]);
            LogErr("inline hook rejected for %p (undecodable insn at +%d): %s", target, n, dump);
            return false;
        }
        if (ripOff >= 0) {
            if (ripCount >= 8) return false;
            rips[ripCount++] = {n + ripOff, n + len};
        }
        n += len;
        if (g_cfg.log_level >= 2) {
            Log("hook %p decode step n=%d len=%d next=%d", target, n - len, len, n);
        }
    }

    uint8_t* relay = AllocNear(fn, 64);
    if (!relay) {
        LogErr("inline hook rejected for %p (no relay within +-2GB)", target);
        return false;
    }

    // Copy displaced instructions, re-basing rip displacements.
    // rip-relative target = insn_end + disp32, so the shift is fn - relay.
    // AllocNear keeps |fn - relay| under 2GB, so the int32 cast is exact.
    memcpy(relay, fn, n);
    const int32_t shift = (int32_t)(fn - relay);
    for (int r = 0; r < ripCount; r++) {
        uint8_t* disp = relay + rips[r].off;
        int32_t oldDisp;
        memcpy(&oldDisp, disp, 4);
        int32_t newDisp = oldDisp + shift;
        memcpy(disp, &newDisp, 4);
    }
    relay[n] = 0xFF; relay[n + 1] = 0x25; // jmp [rip+0]
    *reinterpret_cast<uint32_t*>(relay + n + 2) = 0;
    *reinterpret_cast<uint64_t*>(relay + n + 6) = reinterpret_cast<uint64_t>(fn + n);

    DWORD old = 0;
    if (!VirtualProtect(fn, n, PAGE_EXECUTE_READWRITE, &old)) return false;
    fn[0] = 0xFF; fn[1] = 0x25; // jmp [rip+0]
    *reinterpret_cast<uint32_t*>(fn + 2) = 0;
    *reinterpret_cast<uint64_t*>(fn + 6) = reinterpret_cast<uint64_t>(hookFn);
    for (int i = 14; i < n; i++) fn[i] = 0x90; // nop padding
    VirtualProtect(fn, n, old, &old);
    FlushInstructionCache(GetCurrentProcess(), fn, n);

    *trampolineOut = relay;
    char dump[80];
    int pos = 0;
    for (int i = 0; i < n && pos < 70; i++) pos += snprintf(dump + pos, 80 - pos, "%02X ", relay[i]);
    Log("inline hooked %p (%d bytes, prologue %s)", target, n, dump);
    return true;
}

typedef void (*Send_t)(void* self, void* eventName, int idx, int ret, void* data, void* mi);
typedef void (*Awake_t)(void* self, void* mi);
typedef void (*Log1_t)(void* message, void* mi);
typedef void (*Log2_t)(void* message, void* context, void* mi);

static Send_t g_origSend = nullptr;
static Awake_t g_origAwake = nullptr;
static Log1_t g_origLog = nullptr;
static Log2_t g_origLogCtx = nullptr;
static Log1_t g_origWarn = nullptr;
static Log2_t g_origWarnCtx = nullptr;
static Log1_t g_origError = nullptr;
static Log2_t g_origErrorCtx = nullptr;

// MaxSDKWindows.Login bypass ----------------------------------------------
// The login page (hot update) calls MaxSDKWindows.Login, which hands control
// to the native FlowSDK. The SDK shows its own account/transfer dialog and
// validates every login against its backend (api-flowhw.feimogames.com),
// which rejects synthetic Steam tickets with "Sign in failed". Short-circuit
// the whole native SDK: build a MaxLoginData and invoke the success callback.

typedef void (*SDKLogin_t)(void* self, void* loginAction, void* chargeAction, void* mi);
static SDKLogin_t g_origSDKLogin = nullptr;
static il2cpp::Class g_loginDataClass = nullptr;

// MaxLoginData field offsets from the il2cpp dump (reference fields).
// Sid is per-machine stable so every device keeps its own account.
static const struct { int off; const char* value; } kLoginDataFields[] = {
    {0x10, "10000"},                    // Uid
    {0x18, nullptr},                    // Sid - filled by MachineSid()
    {0x20, "Player"},                   // DisplayName
    {0x28, ""},                         // PhotoUrl
    {0x30, "0"},                        // RegTime
    {0x38, ""},                         // Country
    {0x40, ""},                         // Region
};

static std::string MachineSid() {
    char name[128] = {};
    char user[128] = {};
    DWORD n = 128;
    GetComputerNameA(name, &n);
    n = GetEnvironmentVariableA("USERNAME", user, 128);
    if (n == 0 || n >= 128) user[0] = '?';
    std::string seed = std::string(name) + "\\" + user;
    uint64_t h = 1469598103934665603ull; // FNV-1a
    for (unsigned char c : seed) {
        h ^= c;
        h *= 1099511628211ull;
    }
    char out[32];
    snprintf(out, sizeof(out), "ps-%016llx", (unsigned long long)h);
    return out;
}

static il2cpp::Object BuildLoginData(const std::string& sid) {
    const il2cpp::Api* api = il2cpp::Get();
    if (!api || !g_loginDataClass) return nullptr;
    il2cpp::Object data = api->object_new(g_loginDataClass);
    if (!data) return nullptr;
    for (const auto& f : kLoginDataFields) {
        const char* value = f.value ? f.value : sid.c_str();
        il2cpp::String s = api->string_new(value);
        if (!s) return nullptr;
        *reinterpret_cast<il2cpp::String*>(reinterpret_cast<uint8_t*>(data) + f.off) = s;
    }
    return data;
}

// Invokes a System.Action<...> delegate through its Invoke method.
static bool InvokeDelegate(void* delegateObj, void** args, int argc) {
    const il2cpp::Api* api = il2cpp::Get();
    if (!api || !delegateObj) return false;
    il2cpp::Class klass = api->object_get_class(delegateObj);
    if (!klass) return false;
    il2cpp::MethodInfo* inv = api->class_get_method_from_name(klass, "Invoke", argc);
    if (!inv) return false;
    void* exc = nullptr;
    api->runtime_invoke(inv, delegateObj, args, &exc);
    if (exc) {
        char* text = il2cpp::ObjectToString(static_cast<il2cpp::Object>(exc));
        LogErr("login callback threw: %s", text ? text : "(unknown)");
        free(text);
        return false;
    }
    return true;
}

static void Hooked_SDKLogin(void* self, void* loginAction, void* chargeAction, void* mi) {
    const std::string sid = MachineSid();
    Log("MaxSDKWindows.Login intercepted, faking success (sid=%s)", sid.c_str());
    il2cpp::Object data = BuildLoginData(sid);
    if (!data) {
        LogErr("failed to build MaxLoginData");
        return;
    }
    int ret = 1; // success convention: pay/check callbacks use 1 = ok
    char msg[8] = "ok";
    void* args[3] = { &ret, il2cpp::NewString(msg), data };
    if (InvokeDelegate(loginAction, args, 3)) {
        Log("login success callback delivered (uid=10000, sid=%s)", sid.c_str());
        return;
    }
    LogErr("failed to invoke login success callback");
}

static il2cpp::MethodInfo* g_receiveMethod = nullptr;
static il2cpp::MethodInfo* g_closeLoadingMethod = nullptr;
static il2cpp::String g_emptyString = nullptr;

static void CallReceive(int idx, int ret, const char* jsonUtf8) {
    void* args[4];
    args[0] = g_emptyString;
    args[1] = &idx;
    args[2] = &ret;
    args[3] = il2cpp::NewString(jsonUtf8);
    il2cpp::InvokeStatic(g_receiveMethod, args);
}

static void CallCloseLoading() {
    il2cpp::InvokeStatic(g_closeLoadingMethod, nullptr);
}

static void MirrorGameLog(const char* level, void* message) {
    char* text = il2cpp::ObjectToString(static_cast<il2cpp::Object>(message));
    if (!text) return;
    // Skip our own chatter re-entering Debug.Log (none currently), keep the
    // console readable by trimming very long payloads.
    if (strlen(text) > 4000) text[4000] = '\0';
    LogGame(level, text);
    free(text);
}

static void Hooked_Log(void* message, void* mi) {
    MirrorGameLog("LOG", message);
    g_origLog(message, mi);
}

static void Hooked_LogCtx(void* message, void* context, void* mi) {
    MirrorGameLog("LOG", message);
    g_origLogCtx(message, context, mi);
}

static void Hooked_Warn(void* message, void* mi) {
    MirrorGameLog("WARN", message);
    g_origWarn(message, mi);
}

static void Hooked_WarnCtx(void* message, void* context, void* mi) {
    MirrorGameLog("WARN", message);
    g_origWarnCtx(message, context, mi);
}

static void Hooked_Error(void* message, void* mi) {
    MirrorGameLog("ERROR", message);
    g_origError(message, mi);
}

static void Hooked_ErrorCtx(void* message, void* context, void* mi) {
    MirrorGameLog("ERROR", message);
    g_origErrorCtx(message, context, mi);
}

static void Hooked_Send(void* self, void* eventName, int idx, int ret, void* data, void* mi) {
    char* ev = il2cpp::StringToUtf8(static_cast<il2cpp::String>(eventName));
    char* payload = data ? il2cpp::StringToUtf8(static_cast<il2cpp::String>(data)) : nullptr;
    if (g_cfg.log_level >= 2) {
        if (payload && strlen(payload) > 200) payload[200] = '\0';
        Log("SDK event: %s idx=%d ret=%d data=%s", ev ? ev : "(null)", idx, ret,
            payload ? payload : "(null)");
    }
    if (ev && g_cfg.hook_login) {
        if (strcmp(ev, "loginSteam") == 0 || strcmp(ev, "loginOAuth") == 0) {
            Log("%s intercepted, replying with synthetic ticket", ev);
            free(ev);
            free(payload);
            // 16 zero bytes -> 32 hex chars; the server does not validate it.
            CallReceive(idx, 1, "{\"ticket\":\"00000000000000000000000000000000\"}");
            CallCloseLoading();
            return;
        }
        if (strcmp(ev, "getCurrentLanguage") == 0) {
            std::string json = "{\"language\":\"" + g_cfg.language + "\",\"steam_id\":\"" +
                               g_cfg.steam_id + "\"}";
            free(ev);
            Log("getCurrentLanguage intercepted");
            CallReceive(idx, 1, json.c_str());
            return;
        }
        if (strcmp(ev, "chargeInit") == 0 || strcmp(ev, "requestReview") == 0) {
            bool charge = strcmp(ev, "chargeInit") == 0;
            free(ev);
            Log("%s suppressed (no Steam)", charge ? "chargeInit" : "requestReview");
            return;
        }
    }
    free(ev);
    free(payload);
    g_origSend(self, eventName, idx, ret, data, mi);
}

// SteamManager.Awake normally initializes Facepunch.Steamworks. We skip it
// and force m_bInitialized (field offset 0x20) so OnCreate passes its check.
static void Hooked_Awake(void* self, void* mi) {
    Log("SteamManager.Awake skipped, forcing m_bInitialized");
    *reinterpret_cast<bool*>(reinterpret_cast<uint8_t*>(self) + 0x20) = true;
}

static void HookClassLog(const char* method, void* hook1, void* hook2,
                         void** tramp1, void** tramp2) {
    il2cpp::Class klass = il2cpp::FindClass("UnityEngine.CoreModule.dll", "UnityEngine", "Debug");
    if (!klass) {
        LogErr("UnityEngine.Debug class not found");
        return;
    }
    il2cpp::MethodInfo* m1 = il2cpp::Get()->class_get_method_from_name(klass, method, 1);
    if (m1) {
        void* fp = *static_cast<void**>(m1); // real entry (methodPointer)
        InlineHookPrologue(fp, hook1, tramp1);
    }
    il2cpp::MethodInfo* m2 = il2cpp::Get()->class_get_method_from_name(klass, method, 2);
    if (m2) {
        void* fp = *static_cast<void**>(m2);
        InlineHookPrologue(fp, hook2, tramp2);
    }
    Log("inline hooks installed for Debug.%s", method);
}

static bool InstallHooks() {
    // SDK events flow: native libmaxsdk -> MaxLibUnity.sendFn ->
    // MaxContextUnity.CallbackHandler -> flow_context_platform.Send() ->
    // MaxContextPlatformSteam.Send (virtual, vtable-dispatched). Verified via
    // logs that a MaxContextUnity.Send methodPointer swap never fires - the
    // vtable and the native sendFn path bypass it entirely. Inline-patch the
    // Steam platform entry instead: patched bytes catch every dispatch form.
    il2cpp::Class platClass = il2cpp::FindClass(
        "FMax_InternationalSteam.Runtime.dll", "", "MaxContextPlatformSteam");
    if (!platClass) {
        LogErr("MaxContextPlatformSteam not found");
        return false;
    }
    il2cpp::MethodInfo* send = il2cpp::Get()->class_get_method_from_name(platClass, "Send", 4);
    if (!send) {
        LogErr("MaxContextPlatformSteam.Send(4) not found");
        return false;
    }
    void* sendFp = *static_cast<void**>(send);
    if (!InlineHookPrologue(sendFp, (void*)&Hooked_Send, (void**)&g_origSend)) {
        LogErr("inline hook on MaxContextPlatformSteam.Send rejected");
        return false;
    }
    Log("inline hooked MaxContextPlatformSteam.Send (orig=%p)", sendFp);

    // MaxSDKWindows.Login: entry point the hot update uses for SDK login. It
    // is a virtual override reached through the interpreter's virtual call,
    // so inline-hook the entry bytes (methodPointer swap would not stick).
    g_loginDataClass = il2cpp::FindClass(
        "FMax_InternationalSteam.Runtime.dll", "", "MaxLoginData");
    if (!g_loginDataClass) {
        LogErr("MaxLoginData class not found");
        return false;
    }
    il2cpp::Class sdkWin = il2cpp::FindClass(
        "FMax_InternationalSteam.Runtime.dll", "", "MaxSDKWindows");
    if (!sdkWin) {
        LogErr("MaxSDKWindows not found");
        return false;
    }
    il2cpp::MethodInfo* sdkLogin =
        il2cpp::Get()->class_get_method_from_name(sdkWin, "Login", 2);
    if (!sdkLogin) {
        LogErr("MaxSDKWindows.Login(2) not found");
        return false;
    }
    void* sdkLoginFp = *static_cast<void**>(sdkLogin);
    if (!InlineHookPrologue(sdkLoginFp, (void*)&Hooked_SDKLogin, (void**)&g_origSDKLogin)) {
        LogErr("inline hook on MaxSDKWindows.Login rejected");
        return false;
    }
    Log("inline hooked MaxSDKWindows.Login (orig=%p)", sdkLoginFp);

    il2cpp::Class steamMgr = il2cpp::FindClass(
        "FMax_InternationalSteam.Runtime.dll", "", "SteamManager");
    if (steamMgr) {
        il2cpp::MethodInfo* awake =
            il2cpp::Get()->class_get_method_from_name(steamMgr, "Awake", 0);
        if (awake) {
            g_origAwake = reinterpret_cast<Awake_t>(SwapMethodPointer(awake, &Hooked_Awake));
            Log("hooked SteamManager.Awake");
        }
    } else {
        Log("SteamManager not found (continuing without Awake hook)");
    }

    il2cpp::Class maxLib = il2cpp::FindClass(
        "FMax_InternationalSteam.Runtime.dll", "", "MaxLibUnity");
    if (!maxLib) {
        LogErr("MaxLibUnity not found");
        return false;
    }
    g_receiveMethod = il2cpp::Get()->class_get_method_from_name(maxLib, "Receive", 4);
    if (!g_receiveMethod) {
        LogErr("MaxLibUnity.Receive(4) not found");
        return false;
    }
    il2cpp::Class maxUtils = il2cpp::FindClass(
        "FMax_InternationalSteam.Runtime.dll", "", "MaxContextUtils");
    if (maxUtils) {
        g_closeLoadingMethod =
            il2cpp::Get()->class_get_method_from_name(maxUtils, "CloseLoading", 0);
    }
    g_emptyString = il2cpp::NewString("");

    if (g_cfg.hook_debug_log) {
        HookClassLog("Log", (void*)&Hooked_Log, (void*)&Hooked_LogCtx,
                     (void**)&g_origLog, (void**)&g_origLogCtx);
        HookClassLog("LogWarning", (void*)&Hooked_Warn, (void*)&Hooked_WarnCtx,
                     (void**)&g_origWarn, (void**)&g_origWarnCtx);
        HookClassLog("LogError", (void*)&Hooked_Error, (void*)&Hooked_ErrorCtx,
                     (void**)&g_origError, (void**)&g_origErrorCtx);
    }
    return true;
}

// -------------------------------------------------------------- work thread

static HMODULE g_selfModule = nullptr;

static bool WaitForGameAssembly(HMODULE* out) {
    for (int i = 0; i < 3000; i++) { // up to 5 minutes, slow disk / DRM first-run
        HMODULE h = GetModuleHandleW(L"GameAssembly.dll");
        if (h) {
            *out = h;
            return true;
        }
        Sleep(100);
    }
    return false;
}

static DWORD WINAPI InitThread(LPVOID) {
    // Everything runs here: AllocConsole, config IO and hooks must stay out
    // of DllMain - the loader lock makes console allocation and synchronous
    // file work risky, and a stuck DllMain leaves the game suspended forever.
    LoadConfig(g_selfModule);
    InitConsole();
    HMODULE ga = nullptr;
    if (!WaitForGameAssembly(&ga)) {
        LogErr("GameAssembly.dll never loaded");
        return 1;
    }
    // il2cpp_init runs during Unity startup; wait until the domain exists.
    // Large installs can take minutes before the scripting runtime is up.
    for (int i = 0; i < 3000; i++) {
        if (il2cpp::Initialize() && il2cpp::Get()->domain_get()) break;
        Sleep(100);
    }
    if (!il2cpp::Get() || !il2cpp::Get()->domain_get()) {
        LogErr("il2cpp domain never appeared");
        return 1;
    }
    // il2cpp_init registers metadata incrementally AFTER the domain object
    // exists. class_from_name during that window AVs (deterministic on cold
    // boots where Defender slows init down). Wait until the assembly list is
    // complete and stable, then leave a grace period before hooking.
    il2cpp::Domain domain = il2cpp::Get()->domain_get();
    size_t last = (size_t)-1;
    int stable = 0;
    for (int i = 0; i < 600; i++) {
        size_t count = 0;
        il2cpp::Get()->domain_get_assemblies(domain, &count);
        if (count == last && count > 0) {
            if (++stable >= 20) break;
        } else {
            stable = 0;
            last = count;
        }
        Sleep(100);
    }
    Sleep(3000);
    Log("metadata stable (%zu assemblies), installing hooks", last);
    // Attach this thread to the il2cpp/GC runtime. The thread exits as soon
    // as the hooks are in, so it MUST detach before returning - a stale GC
    // thread registration makes later collections abort with "Collecting
    // from unknown thread".
    il2cpp::Thread attached = il2cpp::Get()->thread_attach(il2cpp::Get()->domain_get());
    if (g_cfg.write_hacker_config) {
        WriteHackerConfig();
    }
    EnsureUnityCacheTemp();
    // Install hooks as early as possible: AOT class metadata is available
    // right after il2cpp_init, well before the login UI or SteamManager init.
    bool hooked = false;
    for (int i = 0; i < 120; i++) {
        if (InstallHooks()) {
            hooked = true;
            break;
        }
        Sleep(500);
    }
    if (attached) {
        il2cpp::Get()->thread_detach(attached);
    }
    Log(hooked ? "patch fully installed" : "hook installation failed");
    return hooked ? 0 : 2;
}

BOOL APIENTRY DllMain(HMODULE module, DWORD reason, LPVOID) {
    if (reason == DLL_PROCESS_ATTACH) {
        DisableThreadLibraryCalls(module);
        g_selfModule = module;
        HANDLE t = CreateThread(nullptr, 0, InitThread, nullptr, 0, nullptr);
        if (t) CloseHandle(t);
    }
    return TRUE;
}

