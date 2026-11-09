#include "il2cpp_api.h"

#include <cstdlib>
#include <cstring>

namespace il2cpp {

static Api g_api = {};
static bool g_ready = false;

#define RESOLVE(name)                                                          \
    do {                                                                       \
        g_api.name = reinterpret_cast<decltype(g_api.name)>(                   \
            GetProcAddress(hMod, "il2cpp_" #name));                            \
        if (!g_api.name) {                                                     \
            return false;                                                      \
        }                                                                      \
    } while (0)

bool Initialize() {
    if (g_ready) {
        return true;
    }
    HMODULE hMod = GetModuleHandleW(L"GameAssembly.dll");
    if (!hMod) {
        return false;
    }
    RESOLVE(domain_get);
    RESOLVE(domain_get_assemblies);
    RESOLVE(assembly_get_image);
    RESOLVE(image_get_name);
    RESOLVE(class_from_name);
    RESOLVE(class_get_method_from_name);
    RESOLVE(object_new);
    RESOLVE(runtime_invoke);
    RESOLVE(string_new);
    RESOLVE(thread_attach);
    RESOLVE(thread_current);
    RESOLVE(thread_detach);
    RESOLVE(object_get_class);
    g_ready = true;
    return true;
}

const Api* Get() {
    return g_ready ? &g_api : nullptr;
}

Class FindClass(const char* imageName, const char* ns, const char* name) {
    const Api* api = Get();
    if (!api) {
        return nullptr;
    }
    Domain domain = api->domain_get();
    if (!domain) {
        return nullptr;
    }
    size_t count = 0;
    Assembly* assemblies = api->domain_get_assemblies(domain, &count);
    if (!assemblies) {
        return nullptr;
    }
    for (size_t i = 0; i < count; i++) {
        Image image = api->assembly_get_image(assemblies[i]);
        if (!image) {
            continue;
        }
        const char* imgName = api->image_get_name(image);
        if (!imgName || _stricmp(imgName, imageName) != 0) {
            continue;
        }
        Class klass = api->class_from_name(image, ns, name);
        if (klass) {
            return klass;
        }
    }
    return nullptr;
}

Object InvokeStatic(const MethodInfo* mi, void** args) {
    const Api* api = Get();
    if (!api || !mi) {
        return nullptr;
    }
    void* exc = nullptr;
    return api->runtime_invoke(mi, nullptr, args, &exc);
}

char* StringToUtf8(String s) {
    if (!s) {
        return nullptr;
    }
    // Il2CppString layout: [sync 0x00][klass 0x08][length int32 0x10][chars 0x14]
    const int32_t length =
        *reinterpret_cast<const int32_t*>(reinterpret_cast<const uint8_t*>(s) + 0x10);
    if (length <= 0) {
        char* empty = static_cast<char*>(malloc(1));
        empty[0] = '\0';
        return empty;
    }
    const wchar_t* wstr = reinterpret_cast<const wchar_t*>(
        reinterpret_cast<const uint8_t*>(s) + 0x14);
    int size = WideCharToMultiByte(CP_UTF8, 0, wstr, length, nullptr, 0, nullptr, nullptr);
    if (size <= 0) {
        return nullptr;
    }
    char* out = static_cast<char*>(malloc(static_cast<size_t>(size) + 1));
    WideCharToMultiByte(CP_UTF8, 0, wstr, length, out, size, nullptr, nullptr);
    out[size] = '\0';
    return out;
}

String NewString(const char* utf8) {
    const Api* api = Get();
    if (!api) {
        return nullptr;
    }
    return api->string_new(utf8);
}

char* ObjectToString(Object obj) {
    const Api* api = Get();
    if (!api) {
        return nullptr;
    }
    if (!obj) {
        char* nullCopy = static_cast<char*>(malloc(7));
        strcpy_s(nullCopy, 7, "(null)");
        return nullCopy;
    }
    Class klass = api->object_get_class(obj);
    if (!klass) {
        return nullptr;
    }
    MethodInfo* toString = api->class_get_method_from_name(klass, "ToString", 0);
    if (!toString) {
        return nullptr;
    }
    void* exc = nullptr;
    Object result = api->runtime_invoke(toString, obj, nullptr, &exc);
    if (exc || !result) {
        return nullptr;
    }
    return StringToUtf8(static_cast<String>(result));
}

} // namespace il2cpp
