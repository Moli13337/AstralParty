# Astral Party client patch

Redirects the INT Steam client to a private server and removes the Steam SDK
login dependency. No game files are modified; everything is injected at
runtime.

## Layout

```
patch/
  build.bat          MSVC (preferred) or MinGW build script
  config/ap_patch.json  patch configuration template
  dll/ap_patch.cpp   patch DLL: config, console log, server redirect, il2cpp hooks
  dll/il2cpp_api.*   dynamic il2cpp binding (GetProcAddress only)
  injector/ap_inject.cpp  suspended-process launcher + remote-thread injector
  stub/steam_api64.cpp    optional steam_api64 replacement (safe "no Steam" answers)
  out/               build output: ap_patch.dll, ap_inject.exe, steam_api64.dll, ap_patch.json
```

## How it works

1. **Server address.** The game's `HackerConfig.TryGetHackerConfigure` reads a
   local `UELgkXDewH6YvC9R.json` (from `persistentDataPath` or
   `StreamingAssets`) before falling back to the remote CDN. The patch writes
   this file with `webServerIpPort` taken from `ap_patch.json`, so every TCP
   frame the client sends lands on your LAN server (default port 8800).
2. **Steam-free login.** The AOT SDK layer
   (`FMax_InternationalSteam.Runtime`) routes SDK calls through
   `MaxContextUnity::Send`. The patch swaps the method's il2cpp
   `MethodInfo.methodPointer` and answers the SDK events locally:
   - `loginSteam` -> synthetic ticket, replying
     `MaxLibUnity.Receive("", idx, 1, {"ticket":"<hex>"})`, exactly the shape
     of the real success path (`MaxContextPlatformSteam.<Login>d__6.MoveNext`).
   - `getCurrentLanguage` -> `{"language":...,"steam_id":...}`.
   - `chargeInit` / `requestReview` -> suppressed (payment/overlay).
3. **Steamworks never initializes.** `SteamManager::Awake` is skipped and
   `m_bInitialized` is forced true, so `MaxContextPlatformSteam::OnCreate`
   passes without Steam running.
4. **Log console.** The patch allocates its own console window
   ("Astral Party Patch Log"). Every patch line and every mirrored
   `UnityEngine.Debug.Log/LogWarning/LogError` call (the hot-update
   GameLogic developer logs, e.g. `[BnSdkManager] ...`) prints there,
   color-coded, plus `ap_patch.log` on disk. The original Unity logging
   behavior is untouched - hooks forward to the real function.
5. The client then connects with `AuthType.China` carrying the SDK sid; the
   server maps `china:<gameId>:<sid>` to an account. Set a distinct
   `steam_id` in the config per player to give each player their own save.

## Deploy

1. Build: `build.bat` (needs VS Build Tools or MinGW-w64). Output lands in
   `patch\out\`.
2. Edit `out\ap_patch.json`:
   - `server_ip` - LAN IP of the machine running the Go server.
   - `server_port` - default `8800` matches the server default.
   - `steam_id` - per-player account key (any decimal string).
3. Start the server (`server\cmd\server`, listens on `:8800`).
4. Launch the game through the injector (run from `patch\out\`):

   ```
   ap_inject.exe "D:\Steam\steamapps\common\Astral Party\8vJXnINT\AstralParty.exe"
   ```

   Add `--steam-stub` to also copy the `steam_api64.dll` stub next to the
   game exe (recommended on LAN machines without Steam):

   ```
   ap_inject.exe --steam-stub "D:\...\AstralParty.exe"
   ```

   or attach to an already running game:

   ```
   ap_inject.exe --pid 12345
   ```

   The injector and `ap_patch.dll` must stay in the same folder; the DLL
   reads `ap_patch.json` from its own directory.
5. Verify: the "Astral Party Patch Log" console opens and ends with
   `patch fully installed`; on the login screen press start - the game
   reaches your server without Steam.

## Config reference

| Key | Default | Meaning |
| --- | --- | --- |
| server_ip | 192.168.1.100 | game server IP |
| server_port | 8800 | game server TCP port |
| log_ip | 127.0.0.1:9999 | remote log sink (redirected away) |
| notice_ip | 127.0.0.1 | notice server |
| cdn_url | http://127.0.0.1/ | hot-update CDN base |
| steam_id | 76561198000000001 | fake Steam id; account key per player |
| language | english | value returned for getCurrentLanguage |
| gm | true | unlocks the in-game GM channel |
| recharge_test | true | enables the test recharge path |
| write_hacker_config | true | write the server redirect JSON |
| hook_login | true | intercept the SDK login events |
| console | true | allocate the patch log console window |
| hook_debug_log | true | mirror UnityEngine.Debug.* into the console |
| log_level | 2 | 0 silent, 1 errors, 2 verbose (every SDK event) |

## Notes

- The game keeps its file hash checks intact - nothing on disk is modified.
  The JSON drop uses the game's own developer override channel.
- The `steam_api64.dll` stub replaces the real one in the game dir and
  disables Steam overlay/achievements for that install; delete it to restore
  original behavior.
- `log_level 2` prints every SDK event and game log line; set `console:
  false` or `log_level: 0` for a silent run (file log still writes at
  error level).
