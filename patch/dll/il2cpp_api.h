#pragma once
// Minimal dynamic binding to the il2cpp runtime exported from GameAssembly.dll.
// Everything is resolved via GetProcAddress at runtime, no import lib needed.

#include <windows.h>
#include <cstdint>

namespace il2cpp {

typedef void* Domain;
typedef void* Assembly;
typedef void* Image;
typedef void* Class;
typedef void* MethodInfo;
typedef void* Object;
typedef void* Thread;
typedef void* String;

struct Api {
    Domain (*domain_get)();
    Assembly* (*domain_get_assemblies)(Domain, size_t*);
    Image (*assembly_get_image)(Assembly);
    const char* (*image_get_name)(Image);
    Class (*class_from_name)(Image, const char* ns, const char* name);
    MethodInfo* (*class_get_method_from_name)(Class, const char*, int);
    Object (*object_new)(Class);
    Object (*runtime_invoke)(const MethodInfo*, void*, void**, void**);
    String (*string_new)(const char*);
    Thread (*thread_attach)(Domain);
    Thread (*thread_current)();
    void (*thread_detach)(Thread);
    Class (*object_get_class)(Object);
};

// Returns nullptr until Initialize succeeds.
const Api* Get();

// Loads GameAssembly.dll (must already be loaded by the game) and resolves
// every function. Returns false if any symbol is missing.
bool Initialize();

// Iterates all loaded assembly images and resolves the class from the first
// matching image name.
Class FindClass(const char* imageName, const char* ns, const char* name);

// Helpers -----------------------------------------------------------------

Object InvokeStatic(const MethodInfo* mi, void** args);

// Reads an il2cpp System.String as UTF-8. Caller frees with free().
char* StringToUtf8(String s);

String NewString(const char* utf8);

// Object.ToString() through the runtime; caller frees with free(). Returns
// "(null)" for null objects.
char* ObjectToString(Object obj);

} // namespace il2cpp
