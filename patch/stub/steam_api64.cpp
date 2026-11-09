// steam_api64 stub.
//
// Optional drop-in replacement for the real Steamworks SDK DLL. Every export
// answers "Steam is not available" instead of crashing:
//   SteamAPI_Init / InitEx -> failure result
//   SteamAPI_RestartAppIfNecessary -> false (prevents Steam DRM relaunch loops)
//   interface getters -> nullptr
//   callbacks / memory hooks -> no-op
// The patch DLL already bypasses the Steam login path, so this stub is pure
// protection for unexpected call sites (native DRM checks, leftover
// Steamworks probes). Only used when explicitly placed next to the game exe.

#include <cstdint>

#define STUB_INT(name)          extern "C" __declspec(dllexport) int         __stdcall name(void) { return 0; }
#define STUB_UINT(name)         extern "C" __declspec(dllexport) uint32_t    __stdcall name(void) { return 0; }
#define STUB_PTR(name)          extern "C" __declspec(dllexport) void*       __stdcall name(void) { return nullptr; }
#define STUB_VOID(name)         extern "C" __declspec(dllexport) void        __stdcall name(void) {}

STUB_INT(SteamAPI_Init)
STUB_VOID(SteamAPI_Shutdown)
STUB_VOID(SteamAPI_RunCallbacks)
STUB_INT(SteamAPI_IsSteamRunning)
STUB_INT(SteamAPI_RestartAppIfNecessary)
STUB_VOID(SteamAPI_ReleaseCurrentThreadMemory)
STUB_VOID(SteamAPI_SetWarningMessageHook)
STUB_VOID(SteamAPI_WriteMiniDump)
STUB_VOID(SteamAPI_SetMiniDumpComment)
STUB_UINT(SteamAPI_GetHSteamUser)
STUB_UINT(SteamAPI_GetHSteamPipe)
STUB_UINT(SteamAPI_GetHSteamUser_Unicode)
STUB_INT(SteamAPI_ManualDispatch_Init)
STUB_VOID(SteamAPI_ManualDispatch_RunFrame)
STUB_INT(SteamAPI_ManualDispatch_GetAPICallResult)
STUB_INT(SteamAPI_ManualDispatch_FreeLastResult)

// SteamAPI_InitEx takes an out-error struct pointer; returning the generic
// failure code (2) keeps Facepunch on its "init failed" path.
extern "C" __declspec(dllexport) int __stdcall SteamAPI_InitEx(void* outError) {
    (void)outError;
    return 2;
}

// Interface getters (unversioned + common versioned aliases). Facepunch
// resolves these by name at DllImport time; returning nullptr is safe as
// long as nothing dereferences them, which requires a successful Init.
STUB_PTR(SteamAPI_SteamUser)
STUB_PTR(SteamAPI_SteamUtils)
STUB_PTR(SteamAPI_SteamFriends)
STUB_PTR(SteamAPI_SteamApps)
STUB_PTR(SteamAPI_SteamUserStats)
STUB_PTR(SteamAPI_SteamRemoteStorage)
STUB_PTR(SteamAPI_SteamMatchmaking)
STUB_PTR(SteamAPI_SteamMatchmakingServers)
STUB_PTR(SteamAPI_SteamNetworking)
STUB_PTR(SteamAPI_SteamGameSearch)
STUB_PTR(SteamAPI_SteamParties)
STUB_PTR(SteamAPI_SteamScreenshots)
STUB_PTR(SteamAPI_SteamController)
STUB_PTR(SteamAPI_SteamUGC)
STUB_PTR(SteamAPI_SteamHTMLSurface)
STUB_PTR(SteamAPI_SteamInventory)
STUB_PTR(SteamAPI_SteamVideo)
STUB_PTR(SteamAPI_SteamParentalSettings)
STUB_PTR(SteamAPI_SteamInput)
STUB_PTR(SteamAPI_SteamMusic)
STUB_PTR(SteamAPI_SteamMusicRemote)

STUB_PTR(SteamAPI_SteamUser_v023)
STUB_PTR(SteamAPI_SteamUser_v022)
STUB_PTR(SteamAPI_SteamUser_v021)
STUB_PTR(SteamAPI_SteamUtils_v010)
STUB_PTR(SteamAPI_SteamUtils_v009)
STUB_PTR(SteamAPI_SteamFriends_v017)
STUB_PTR(SteamAPI_SteamFriends_v016)
STUB_PTR(SteamAPI_SteamApps_v008)
STUB_PTR(SteamAPI_SteamUserStats_v015)
STUB_PTR(SteamAPI_SteamUserStats_v014)
STUB_PTR(SteamAPI_SteamRemoteStorage_v016)
STUB_PTR(SteamAPI_SteamMatchmaking_v009)
STUB_PTR(SteamAPI_SteamNetworking_v006)
STUB_PTR(SteamAPI_SteamUGC_v016)
STUB_PTR(SteamAPI_SteamUGC_v015)
STUB_PTR(SteamAPI_SteamInput_v002)
STUB_PTR(SteamAPI_SteamInventory_v003)
STUB_PTR(SteamAPI_SteamController_v008)
