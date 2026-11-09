import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent / "proto"


def collect_type_names(path: Path) -> set:
    text = path.read_text(encoding="utf-8")
    names = set()
    for m in re.finditer(r"^\s*(?:message|enum)\s+(\w+)", text, re.M):
        names.add(m.group(1))
    return names


def qualify(text: str, prefix: str, own: set, targets: set) -> str:
    out_lines = []
    type_ref = re.compile(
        r"^(\s*(?:repeated\s+|optional\s+)?)([A-Z]\w*)(\s+[a-z_]\w*\s*=\s*)"
    )
    map_ref = re.compile(r"(\bmap\s*<\s*[\w.]+\s*,\s*)([A-Z]\w*)(\s*>)")
    for line in text.splitlines():
        if line.lstrip().startswith("//") or line.lstrip().startswith("*"):
            out_lines.append(line)
            continue
        def sub_typed(m):
            name = m.group(2)
            if name in targets and name not in own:
                return f"{m.group(1)}{prefix}.{name}{m.group(3)}"
            return m.group(0)
        def sub_map(m):
            name = m.group(2)
            if name in targets and name not in own:
                return f"{m.group(1)}{prefix}.{name}{m.group(3)}"
            return m.group(0)
        line = map_ref.sub(sub_map, line)
        line = type_ref.sub(sub_typed, line)
        out_lines.append(line)
    return "\n".join(out_lines) + "\n"


def main() -> int:
    model_names = collect_type_names(ROOT / "model" / "model.proto")
    code_names = collect_type_names(ROOT / "errcode" / "code.proto")
    proto_path = ROOT / "protocol.proto"
    own = collect_type_names(proto_path)
    text = proto_path.read_text(encoding="utf-8")

    text = qualify(text, "model", own, model_names)
    text = qualify(text, "code", own, code_names)

    proto_path.write_text(text, encoding="utf-8")
    print(f"qualified refs: model={len(model_names)} types, code={len(code_names)} types")
    return 0


if __name__ == "__main__":
    sys.exit(main())
