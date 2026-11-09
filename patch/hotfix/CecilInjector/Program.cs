using System;
using System.IO;
using System.Linq;
using Mono.Cecil;
using Mono.Cecil.Cil;

// Injects diagnostic logging into the hot-update DLL. Only trivial IL and
// Debug.LogError(Object) are used: every extra BCL dependency risks being
// stripped from the AOT (PlatformNotSupportedException) or misinterpreted
// (raw value-type calls crash the HybridCLR interpreter natively).
static class Program
{
    static int DumpIL(string dllPath, string nameSubstring)
    {
        var asm = AssemblyDefinition.ReadAssembly(dllPath, new ReaderParameters
        {
            SymbolReaderProvider = null,
        });
        foreach (var t in asm.MainModule.Types)
        {
            foreach (var nested in t.NestedTypes)
            {
                if (!nested.Name.Contains(nameSubstring)) continue;
                var mn = nested.Methods.FirstOrDefault(m => m.Name == "MoveNext");
                if (mn == null) continue;
                Console.WriteLine($"===== {nested.FullName} =====");
                foreach (var v in mn.Body.Variables)
                {
                    Console.WriteLine($"  V{v.Index}: {v.VariableType.Name}");
                }
                foreach (var i in mn.Body.Instructions)
                {
                    string op = i.Operand switch
                    {
                        Instruction target => $"IL_{target.Offset:X4}",
                        Instruction[] targets => string.Join(" ", targets.Select(x => $"IL_{x.Offset:X4}")),
                        MethodReference mr => mr.DeclaringType.Name + "::" + mr.Name,
                        FieldReference fr => fr.DeclaringType.Name + "::" + fr.Name,
                        _ => i.Operand?.ToString() ?? "",
                    };
                    Console.WriteLine($"  IL_{i.Offset:X4} {i.OpCode} {op}");
                }
            }
        }
        return 0;
    }

    static int Main(string[] args)
    {
        if (args.Length >= 2 && args[0] == "dump")
        {
            return DumpIL(args[1], args.Length >= 3 ? args[2] : "");
        }
        if (args.Length < 3)
        {
            Console.Error.WriteLine("usage: CecilInjector <in.dll> <out.dll> <refsDir>\n"
                + "       CecilInjector dump <dll> <nestedTypeNameSubstring>");
            return 2;
        }
        string inPath = Path.GetFullPath(args[0]);
        string outPath = Path.GetFullPath(args[1]);
        string refsDir = Path.GetFullPath(args[2]);

        var resolver = new DefaultAssemblyResolver();
        resolver.AddSearchDirectory(Path.GetDirectoryName(inPath));
        resolver.AddSearchDirectory(refsDir);
        var asm = AssemblyDefinition.ReadAssembly(inPath, new ReaderParameters
        {
            AssemblyResolver = resolver,
            SymbolReaderProvider = null,
        });
        var mod = asm.MainModule;
        var logError = DebugLogRef(mod);

        var crpc = mod.GetType("Core.Net.ConnectS2CRPC")
            ?? throw new InvalidDataException("type Core.Net.ConnectS2CRPC not found");

        var pushSm = crpc.NestedTypes.FirstOrDefault(t => t.Name.StartsWith("<PushConnectS2CCallBack>d__"))
            ?? throw new InvalidDataException("nested <PushConnectS2CCallBack>d__* not found");
        var pushMn = pushSm.Methods.FirstOrDefault(m => m.Name == "MoveNext")
            ?? throw new InvalidDataException("MoveNext not found on " + pushSm.Name);
        InstrumentEntry(pushMn, logError, "[APHOOK] [entry]", "errId");
        int catches = InstrumentCatches(pushMn, logError, ExceptionToStringRef(mod));
        Console.WriteLine($"hooked {pushSm.FullName}: entry + {catches} catch handler(s)");

        var nm = mod.GetType("Core.Net.NetManager")
            ?? throw new InvalidDataException("type Core.Net.NetManager not found");
        var relogSm = nm.NestedTypes.FirstOrDefault(t => t.Name.StartsWith("<ReLogin>d__"))
            ?? throw new InvalidDataException("nested <ReLogin>d__* not found");
        var relogMn = relogSm.Methods.FirstOrDefault(m => m.Name == "MoveNext")
            ?? throw new InvalidDataException("MoveNext not found on " + relogSm.Name);
        InstrumentEntry(relogMn, logError, "[APHOOK] [relogin]", null);
        Console.WriteLine($"hooked {relogSm.FullName}: entry");

        var bag = mod.GetType("GameLogic.BagContainer")
            ?? throw new InvalidDataException("type GameLogic.BagContainer not found");
        var getIds = bag.Methods.FirstOrDefault(m => m.Name == "GetIDs" && m.Parameters.Count == 1)
            ?? throw new InvalidDataException("BagContainer.GetIDs not found");
        PatchGetIDs(getIds, mod);
        Console.WriteLine("patched BagContainer.GetIDs: null -> empty List<int>");

        var storeData = mod.GetType("GameLogic.StoreData")
            ?? throw new InvalidDataException("type GameLogic.StoreData not found");
        var getRandomGoods = storeData.Methods.FirstOrDefault(m => m.Name == "GetRandomGoodsIdsByType")
            ?? throw new InvalidDataException("StoreData.GetRandomGoodsIdsByType not found");
        PatchNullReturn(getRandomGoods, mod, storeData, "randGoodsDict", 1);
        Console.WriteLine("patched StoreData.GetRandomGoodsIdsByType: null -> empty RepeatedField<int>");

        var rpcMsg = mod.GetType("Core.Net.RPCMsgManager")
            ?? throw new InvalidDataException("type Core.Net.RPCMsgManager not found");
        var rx = rpcMsg.Methods.FirstOrDefault(m => m.Name == "ReceiveRPCCallStatic")
            ?? throw new InvalidDataException("RPCMsgManager.ReceiveRPCCallStatic not found");
        InstrumentRxLog(rx, logError, mod);
        Console.WriteLine("hooked RPCMsgManager.ReceiveRPCCallStatic: rx cmd log");

        var getMessage = ExceptionToStringRef(mod);
        HookStateMachine(rpcMsg, "<DealOnPushRPCMsg>d__", logError, getMessage, "dispatch");
        HookStateMachine(rpcMsg, "<ProcessNextPushRPCMsg>d__", logError, getMessage, "pushqueue");

        var tutorial = mod.GetType("GameLogic.TutorialLogic")
            ?? throw new InvalidDataException("type GameLogic.TutorialLogic not found");
        HookStateMachine(tutorial, "<StartTutorial>d__", logError, getMessage, "starttutorial");

        var tutorialWin = mod.GetType("UI.TutorialWindow")
            ?? throw new InvalidDataException("type UI.TutorialWindow not found");
        HookStateMachine(tutorialWin, "<TryShowSelectHero>d__", logError, getMessage, "selecthero");
        HookStateMachine(tutorialWin, "<TryShowTutorial>d__", logError, getMessage, "showtutorial");

        var hbRpc = mod.GetType("Core.Net.HeartbeatS2CRPC")
            ?? throw new InvalidDataException("type Core.Net.HeartbeatS2CRPC not found");
        var hbPush = hbRpc.Methods.FirstOrDefault(m => m.Name == "PushHeartbeatS2CCallBack")
            ?? throw new InvalidDataException("HeartbeatS2CRPC.PushHeartbeatS2CCallBack not found");
        var hbIns = hbPush.Body.Instructions;
        hbIns.Insert(0, Instruction.Create(OpCodes.Call, logError));
        hbIns.Insert(0, Instruction.Create(OpCodes.Ldstr, "[APHOOK] [hb] push callback reached"));
        Console.WriteLine("hooked HeartbeatS2CRPC.PushHeartbeatS2CCallBack: entry");

        asm.Write(outPath, new WriterParameters { SymbolWriterProvider = null });
        SelfCheck(outPath);
        Console.WriteLine("written: " + outPath);
        return 0;
    }

    static MethodReference DebugLogRef(ModuleDefinition mod)
    {
        string[] names = { "UnityEngine.CoreModule", "UnityEngine" };
        foreach (var n in names)
        {
            var asmRef = mod.AssemblyReferences.FirstOrDefault(a => a.Name == n);
            if (asmRef == null) continue;
            var t = new TypeReference("UnityEngine", "Debug", mod, asmRef);
            var m = new MethodReference("LogError", mod.TypeSystem.Void, t) { HasThis = false };
            m.Parameters.Add(new ParameterDefinition(mod.TypeSystem.Object));
            return m;
        }
        throw new InvalidDataException("no UnityEngine assembly reference found");
    }

    static FieldDefinition Field(TypeDefinition sm, string name)
    {
        var f = sm.Fields.FirstOrDefault(x => x.Name == name)
            ?? throw new InvalidDataException($"field {name} not found on {sm.Name}");
        return f;
    }

    // Entry marker: LogError(prefix); [LogError(field);] LogError(boxed state).
    // Each LogError must be a self-contained ldstr/ldarg+box/call group - a
    // shared call leaves the prefix string dangling on the eval stack and the
    // HybridCLR interpreter aborts state-machine methods on the imbalance.
    static void InstrumentEntry(MethodDefinition moveNext, MethodReference logError, string prefix, string extraField)
    {
        var decl = (TypeDefinition)moveNext.DeclaringType;
        var state = Field(decl, "<>1__state");
        var ins = moveNext.Body.Instructions;

        Instruction[][] groups =
        {
            new[] { Instruction.Create(OpCodes.Ldstr, prefix), Instruction.Create(OpCodes.Call, logError) },
            new[]
            {
                Instruction.Create(OpCodes.Ldarg_0),
                Instruction.Create(OpCodes.Ldfld, state),
                Instruction.Create(OpCodes.Box, moveNext.Module.TypeSystem.Int32),
                Instruction.Create(OpCodes.Call, logError),
            },
        };
        if (extraField != null)
        {
            groups = new[]
            {
                groups[0],
                new[]
                {
                    Instruction.Create(OpCodes.Ldstr, "[APHOOK] " + prefix.Substring(9) + " " + extraField + "="),
                    Instruction.Create(OpCodes.Call, logError),
                },
                new[]
                {
                    Instruction.Create(OpCodes.Ldarg_0),
                    Instruction.Create(OpCodes.Ldfld, Field(decl, extraField)),
                    Instruction.Create(OpCodes.Box, moveNext.Module.TypeSystem.Int32),
                    Instruction.Create(OpCodes.Call, logError),
                },
                groups[1],
            };
        }
        int pos = 0;
        foreach (var group in groups)
        {
            foreach (var instr in group)
            {
                ins.Insert(pos++, instr);
            }
        }
    }

    // Handler marker: dup; callvirt ex.get_Message; LogError(msg); the original
    // handler keeps [ex] on the stack. Printing the message string works with
    // the string-only output channel, unlike printing the exception object
    // (which shows as a raw heap pointer).
    static int InstrumentCatches(MethodDefinition moveNext, MethodReference logError, MethodReference getMessage)
    {
        var body = moveNext.Body;
        int hooked = 0;
        foreach (var eh in body.ExceptionHandlers.ToArray())
        {
            if (eh.CatchType == null || eh.CatchType.FullName != "System.Exception") continue;
            var oldStart = eh.HandlerStart;
            int idx = body.Instructions.IndexOf(oldStart);
            var call = Instruction.Create(OpCodes.Call, logError);
            var msg = Instruction.Create(OpCodes.Callvirt, getMessage);
            var dup = Instruction.Create(OpCodes.Dup);
            body.Instructions.Insert(idx, call);
            body.Instructions.Insert(idx, msg);
            body.Instructions.Insert(idx, dup);
            // dispatch jumps to HandlerStart, so the region must begin at the Dup
            foreach (var h in body.ExceptionHandlers)
            {
                if (h.HandlerStart == oldStart) h.HandlerStart = dup;
                if (h.HandlerEnd == oldStart) h.HandlerEnd = dup;
            }
            hooked++;
        }
        return hooked;
    }

    // Entry marker for a plain method: LogError(prefix); LogError(boxed CMDID).
    static void InstrumentRxLog(MethodDefinition method, MethodReference logError, ModuleDefinition mod)
    {
        var frameType = (TypeDefinition)method.Parameters[0].ParameterType.Resolve()
            ?? throw new InvalidDataException("ReceiveRPCCallStatic: cannot resolve Frame type");
        var cmdField = frameType.Fields.FirstOrDefault(f => f.Name == "CMDID")
            ?? throw new InvalidDataException("field CMDID not found on Frame");
        var ins = method.Body.Instructions;
        var group = new[]
        {
            Instruction.Create(OpCodes.Ldstr, "[APHOOK] [rx] cmd="),
            Instruction.Create(OpCodes.Call, logError),
            Instruction.Create(OpCodes.Ldarg_1),
            Instruction.Create(OpCodes.Ldfld, cmdField),
            Instruction.Create(OpCodes.Box, mod.TypeSystem.Int32),
            Instruction.Create(OpCodes.Call, logError),
        };
        for (int i = 0; i < group.Length; i++)
        {
            ins.Insert(i, group[i]);
        }
    }

    // Entry + catch markers on a compiler-generated state machine whose body
    // is missing from the decomp. Catches print ex.Message through the string
    // channel; UniTask.Forget-swallowed exceptions surface here.
    static void HookStateMachine(TypeDefinition owner, string stateMachinePrefix,
        MethodReference logError, MethodReference getMessage, string tag)
    {
        var sm = owner.NestedTypes.FirstOrDefault(t => t.Name.StartsWith(stateMachinePrefix))
            ?? throw new InvalidDataException($"nested {stateMachinePrefix}* not found on {owner.Name}");
        var moveNext = sm.Methods.FirstOrDefault(m => m.Name == "MoveNext")
            ?? throw new InvalidDataException("MoveNext not found on " + sm.Name);
        InstrumentEntry(moveNext, logError, "[APHOOK] [" + tag + "]", null);
        int catches = InstrumentCatches(moveNext, logError, getMessage);
        Console.WriteLine($"hooked {sm.FullName}: entry + {catches} catch handler(s)");
    }

    static MethodReference ExceptionToStringRef(ModuleDefinition mod)
    {
        var exc = new TypeReference("System", "Exception", mod, mod.TypeSystem.CoreLibrary);
        var m = new MethodReference("ToString", mod.TypeSystem.String, exc)
        {
            HasThis = true,
        };
        return mod.ImportReference(m);
    }

    // GetIDs returns null for item types missing from the bag, and callers
    // foreach straight over the result (NRE on empty bag). Replace the single
    // ldnull with an empty List<int>.
    static void PatchGetIDs(MethodDefinition getIds, ModuleDefinition mod)
    {
        var ins = getIds.Body.Instructions;
        var nulls = ins.Where(i => i.OpCode == OpCodes.Ldnull).ToList();
        if (nulls.Count != 1)
            throw new InvalidDataException($"GetIDs: expected exactly 1 ldnull, found {nulls.Count}");
        var fld = getIds.DeclaringType.Fields.FirstOrDefault(f => f.Name == "_itemTypeList")
            ?? throw new InvalidDataException("field _itemTypeList not found");
        var listInt = ((GenericInstanceType)fld.FieldType).GenericArguments[1];
        var ctor = new MethodReference(".ctor", mod.TypeSystem.Void, listInt) { HasThis = true };
        nulls[0].OpCode = OpCodes.Newobj;
        nulls[0].Operand = mod.ImportReference(ctor);
    }

    // Generic form of PatchGetIDs: a lookup method that returns null when the
    // map lacks the key, with callers dereferencing the result. The empty
    // container type is taken from the map field's generic argument at
    // genericArgIndex.
    static void PatchNullReturn(MethodDefinition method, ModuleDefinition mod, TypeDefinition owner,
        string mapFieldName, int genericArgIndex)
    {
        var ins = method.Body.Instructions;
        var nulls = ins.Where(i => i.OpCode == OpCodes.Ldnull).ToList();
        if (nulls.Count != 1)
            throw new InvalidDataException($"{method.Name}: expected exactly 1 ldnull, found {nulls.Count}");
        var fld = owner.Fields.FirstOrDefault(f => f.Name == mapFieldName)
            ?? throw new InvalidDataException($"field {mapFieldName} not found on {owner.Name}");
        var emptyType = ((GenericInstanceType)fld.FieldType).GenericArguments[genericArgIndex];
        var ctor = new MethodReference(".ctor", mod.TypeSystem.Void, emptyType) { HasThis = true };
        nulls[0].OpCode = OpCodes.Newobj;
        nulls[0].Operand = mod.ImportReference(ctor);
    }

    static void SelfCheck(string dllPath)
    {
        var asm = AssemblyDefinition.ReadAssembly(dllPath, new ReaderParameters
        {
            SymbolReaderProvider = null,
        });
        var crpc = asm.MainModule.GetType("Core.Net.ConnectS2CRPC")
            ?? throw new InvalidDataException("selfcheck: ConnectS2CRPC lost");
        var mn = crpc.NestedTypes.First(t => t.Name.StartsWith("<PushConnectS2CCallBack>d__")).Methods
            .First(m => m.Name == "MoveNext");
        if (!mn.Body.Instructions.Any(i => i.OpCode == OpCodes.Ldstr && (string)i.Operand == "[APHOOK] [entry]"))
            throw new InvalidDataException("selfcheck: entry hook lost");
        int dupStarts = mn.Body.ExceptionHandlers.Count(eh => eh.CatchType != null
            && eh.CatchType.FullName == "System.Exception"
            && eh.HandlerStart.OpCode == OpCodes.Dup);
        if (dupStarts == 0)
            throw new InvalidDataException("selfcheck: catch hook lost");
        var bag2 = asm.MainModule.GetType("GameLogic.BagContainer")
            ?? throw new InvalidDataException("selfcheck: BagContainer lost");
        var getIds2 = bag2.Methods.First(m => m.Name == "GetIDs" && m.Parameters.Count == 1);
        if (!getIds2.Body.Instructions.Any(i => i.OpCode == OpCodes.Newobj))
            throw new InvalidDataException("selfcheck: GetIDs newobj lost");
        var rx2 = asm.MainModule.GetType("Core.Net.RPCMsgManager").Methods
            .First(m => m.Name == "ReceiveRPCCallStatic");
        if (!rx2.Body.Instructions.Any(i => i.OpCode == OpCodes.Ldstr && (string)i.Operand == "[APHOOK] [rx] cmd="))
            throw new InvalidDataException("selfcheck: rx hook lost");
        int msgHooks = mn.Body.ExceptionHandlers.Count(eh => eh.CatchType != null
            && eh.CatchType.FullName == "System.Exception"
            && eh.HandlerStart.OpCode == OpCodes.Dup
            && eh.HandlerStart.Next.OpCode == OpCodes.Callvirt);
        var hb2 = asm.MainModule.GetType("Core.Net.HeartbeatS2CRPC")
            ?? throw new InvalidDataException("selfcheck: HeartbeatS2CRPC lost");
        var hb2Push = hb2.Methods.First(m => m.Name == "PushHeartbeatS2CCallBack");
        if (!hb2Push.Body.Instructions.Any(i => i.OpCode == OpCodes.Ldstr && ((string)i.Operand).StartsWith("[APHOOK] [hb]")))
            throw new InvalidDataException("selfcheck: hb hook lost");
        Console.WriteLine($"selfcheck ok: entry hook present, {dupStarts} catch hook(s) ({msgHooks} with message), rx hook present, hb hook present, GetIDs patched");
    }
}
