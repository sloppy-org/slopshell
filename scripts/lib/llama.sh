#!/usr/bin/env bash

tabura_llama_library_var() {
    case "$(uname -s)" in
        Darwin) printf '%s' "DYLD_LIBRARY_PATH" ;;
        *) printf '%s' "LD_LIBRARY_PATH" ;;
    esac
}

tabura_llama_prepend_library_dirs() {
    local candidate="$1"
    local candidate_dir candidate_real prefix lib_var current
    local -a dirs=()
    local seen=""

    if [ ! -e "$candidate" ]; then
        return 0
    fi

    candidate_real="$(cd "$(dirname "$candidate")" && pwd)/$(basename "$candidate")"
    candidate_dir="$(dirname "$candidate_real")"
    dirs+=("$candidate_dir" "$candidate_dir/../lib" "$candidate_dir/../lib64")

    if command -v brew >/dev/null 2>&1; then
        prefix="$(brew --prefix llama.cpp 2>/dev/null || true)"
        if [ -n "$prefix" ]; then
            dirs+=("$prefix/lib")
        fi
    fi

    lib_var="$(tabura_llama_library_var)"
    current="${!lib_var-}"

    local dir new_path=""
    for dir in "${dirs[@]}"; do
        [ -d "$dir" ] || continue
        dir="$(cd "$dir" && pwd)"
        case ":$seen:" in
            *":$dir:"*) continue ;;
        esac
        if [ -n "$seen" ]; then
            seen="${seen}:"
        fi
        seen="${seen}${dir}"
        if [ -n "$new_path" ]; then
            new_path="${new_path}:"
        fi
        new_path="${new_path}${dir}"
    done

    if [ -n "$current" ]; then
        new_path="${new_path:+${new_path}:}${current}"
    fi
    if [ -n "$new_path" ]; then
        printf -v "$lib_var" '%s' "$new_path"
        export "$lib_var"
    fi
}

TABURA_LLAMA_LAST_ERROR=""

tabura_llama_server_usable() {
    local candidate="$1"
    local output

    TABURA_LLAMA_LAST_ERROR=""
    if [ ! -x "$candidate" ]; then
        TABURA_LLAMA_LAST_ERROR="candidate is not executable: $candidate"
        return 1
    fi

    tabura_llama_prepend_library_dirs "$candidate"
    if output="$("$candidate" --version 2>&1)"; then
        return 0
    fi

    TABURA_LLAMA_LAST_ERROR="$(printf '%s' "$output" | head -n 1)"
    return 1
}

tabura_llama_server_candidates() {
    local explicit resolved prefix
    local -a candidates=()
    local seen=""

    if [ -n "${LLAMA_SERVER_BIN:-}" ]; then
        if [ -x "$LLAMA_SERVER_BIN" ]; then
            candidates+=("$LLAMA_SERVER_BIN")
        elif resolved="$(command -v "$LLAMA_SERVER_BIN" 2>/dev/null)"; then
            candidates+=("$resolved")
        fi
    fi

    while IFS= read -r resolved; do
        [ -n "$resolved" ] && candidates+=("$resolved")
    done < <(type -aP llama-server 2>/dev/null || true)

    explicit="${HOME}/.local/llama.cpp/llama-server"
    if [ -x "$explicit" ]; then
        candidates+=("$explicit")
    fi

    if command -v brew >/dev/null 2>&1; then
        prefix="$(brew --prefix llama.cpp 2>/dev/null || true)"
        if [ -n "$prefix" ] && [ -x "$prefix/bin/llama-server" ]; then
            candidates+=("$prefix/bin/llama-server")
        fi
    fi

    local candidate
    for candidate in "${candidates[@]}"; do
        [ -n "$candidate" ] || continue
        case ":$seen:" in
            *":$candidate:"*) continue ;;
        esac
        if [ -n "$seen" ]; then
            seen="${seen}:"
        fi
        seen="${seen}${candidate}"
        printf '%s\n' "$candidate"
    done
}

tabura_find_llama_server() {
    local candidate
    local last_error=""

    while IFS= read -r candidate; do
        [ -n "$candidate" ] || continue
        if tabura_llama_server_usable "$candidate"; then
            printf '%s' "$candidate"
            return 0
        fi
        last_error="$TABURA_LLAMA_LAST_ERROR"
    done < <(tabura_llama_server_candidates)

    TABURA_LLAMA_LAST_ERROR="$last_error"
    return 1
}
