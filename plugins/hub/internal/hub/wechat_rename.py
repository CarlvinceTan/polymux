"""Calls WeChat's own group rename inside the running app.

The address belongs to one exact build, so the loaded image's UUID and the
function's first instruction must both match before anything is called.
"""

import json
import os
import re
import struct

import lldb


def _write(process, address, value):
    error = lldb.SBError()
    if process.WriteMemory(address, value, error) != len(value) or error.Fail():
        raise RuntimeError(f"target write failed: {error}")


def _native_string(process, value, allocations):
    # libc++ on arm64: up to 22 bytes inline with the length in byte 23, otherwise
    # pointer, length and capacity with the top bit set. The callee copies both.
    encoded = value.encode("utf-8")
    error = lldb.SBError()
    size = 24 + len(encoded) + 1
    address = process.AllocateMemory(size, lldb.ePermissionsReadable | lldb.ePermissionsWritable, error)
    if error.Fail() or address in (0, lldb.LLDB_INVALID_ADDRESS):
        raise RuntimeError(f"target allocation failed: {error}")
    allocations.append(address)
    header = (encoded.ljust(23, b"\0") + bytes([len(encoded)])) if len(encoded) <= 22 else struct.pack(
        "<QQQ", address + 24, len(encoded), (len(encoded) + 1) | (1 << 63))
    _write(process, address, header + encoded + b"\0")
    return address


def _module_base(target, uuid):
    for module in target.module_iter():
        if (module.GetUUIDString() or "").strip().upper() == uuid:
            address = module.GetObjectFileHeaderAddress().GetLoadAddress(target)
            if address not in (None, lldb.LLDB_INVALID_ADDRESS):
                return address
    raise RuntimeError("wechat_build_not_supported")


def _request():
    with open(os.environ["POLYMUX_WECHAT_RENAME_REQUEST"], encoding="utf-8") as stream:
        request = json.load(stream)
    if not re.fullmatch(request["recipientPattern"], request["recipient"]):
        raise RuntimeError("wechat_group_invalid")
    name = request["name"]
    if not name.strip() or len(name.encode("utf-8")) > 1024 or re.search(r"[\x00-\x1f\x7f]", name):
        raise RuntimeError("wechat_name_invalid")
    return request


def rename(debugger, _command, result, _dict):
    payload = {"ok": False, "reason": "wechat_group_rename_failed"}
    allocations = []
    process = None
    try:
        request = _request()
        target = debugger.GetSelectedTarget()
        process = target.GetProcess()
        if not process.IsValid() or process.GetState() != lldb.eStateStopped:
            raise RuntimeError("wechat_not_stopped")
        threads = [process.GetThreadAtIndex(index) for index in range(process.GetNumThreads())]
        main = [thread for thread in threads if thread.GetQueueName() == "com.apple.main-thread"]
        if len(main) != 1:
            raise RuntimeError("wechat_main_thread_unavailable")
        function = _module_base(target, request["uuid"]) + request["offset"]
        error = lldb.SBError()
        first = process.ReadMemory(function, len(bytes.fromhex(request["entry"])), error)
        if error.Fail() or bytes(first).hex() != request["entry"]:
            raise RuntimeError("wechat_build_not_supported")
        chat = _native_string(process, request["recipient"], allocations)
        name = _native_string(process, request["name"], allocations)
        options = lldb.SBExpressionOptions()
        options.SetLanguage(lldb.eLanguageTypeObjC_plus_plus)
        options.SetTimeoutInMicroSeconds(8_000_000)
        value = main[0].GetFrameAtIndex(0).EvaluateExpression(
            f"({{ ((void (*)(void *,const void *,const void *)){function}ULL)"
            f"((void *)0,(const void *){chat}ULL,(const void *){name}ULL); 1; }})", options)
        if value.GetError().Fail():
            raise RuntimeError(value.GetError().GetCString() or "wechat_call_failed")
        if value.GetValueAsSigned() != 1:
            raise RuntimeError("wechat_call_did_not_return")
        payload = {"ok": True}
    except Exception as error:
        payload = {"ok": False, "reason": str(error)}
    finally:
        if process and process.IsValid() and process.GetState() == lldb.eStateStopped:
            for address in allocations:
                process.DeallocateMemory(address)
    with open(os.environ["POLYMUX_WECHAT_RENAME_STATUS"], "w", encoding="utf-8") as stream:
        json.dump(payload, stream)
    result.AppendMessage(json.dumps(payload))


def __lldb_init_module(debugger, _dict):
    debugger.HandleCommand(f"command script add -f {__name__}.rename polymux-wechat-rename")
