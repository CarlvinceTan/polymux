#!/usr/bin/env python3
"""Build and check the on-device GLiDE dictation gate. See docs/speech.md.

Usage: scripts/glide.py oracle <GLiDE-0.4B checkout>        rescore the oracle with the PyTorch model
       scripts/glide.py fit                                 fit the gate constants on the oracle
       scripts/glide.py convert <GLiDE-0.4B checkout> <out> convert to out/glide-0.4b.mlpackage
       scripts/glide.py parity <glide-0.4b.mlpackage>       check the Core ML model against the oracle
       scripts/glide.py pack <glide-0.4b.mlpackage>         write dist/models/glide-0.4b.tar

The checkout is https://huggingface.co/fastino/GLiDE-0.4B. Every step but fit and pack needs
Python 3.12 with torch 2.15.0, transformers 5.23.0, coremltools 9.0 and numpy 2.5.4.
"""
import hashlib
import json
import math
import os
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ORACLE = os.path.join(ROOT, "packages/SpeechKit/Bench/oracle.json")
LICENSES = os.path.join(ROOT, "packages/SpeechKit/Sources/SpeechKit/Resources")
PACKAGE = "glide-0.4b.mlpackage"
SEQ = 256
# The prompt GlidePrompt.swift rebuilds; the oracle's token ids hold the two in step.
QUESTION = {"type": "noul",
            "instructions": "Does this speech-to-text transcript contain a word or phrase that was probably misrecognised?",
            "criteria": {"true": "At least one word looks misheard, out of place or garbled.",
                         "false": "Every word fits and the sentence reads naturally."}}


def load_oracle():
    with open(ORACLE) as f:
        return json.load(f)["rows"]


def save_oracle(rows):
    with open(ORACLE, "w") as f:
        f.write('{"seq": %d, "rows": [\n' % SEQ)
        f.write(",\n".join(json.dumps(r, ensure_ascii=False, separators=(",", ":")) for r in rows))
        f.write("\n]}\n")


def load_model(checkout):
    """The PyTorch encoder, its tokenizer, and a function from transcript to prompt ids."""
    import torch
    from transformers import AutoTokenizer
    sys.path.insert(0, checkout)
    from glide.encoder import load as load_encoder
    from glide.prompt import Prompter, options_for

    with open(os.path.join(checkout, "decision_config.json")) as f:
        cfg = json.load(f)
    net, tok = load_encoder(checkout, cfg, device="cpu", dtype=torch.float32)
    net.model.config._attn_implementation = "eager"
    net.eval()
    prompter = Prompter(AutoTokenizer.from_pretrained(os.path.join(checkout, "render_tokenizer")), cfg["codes"])
    options = options_for(QUESTION)

    def wrap(transcript):
        # The tokenizer adds [CLS] and [SEP] itself.
        ids = tok([prompter.text(transcript, QUESTION, options)], add_special_tokens=False)["input_ids"][0]
        if len(ids) > SEQ:
            raise ValueError("prompt too long: %d tokens" % len(ids))
        return ids

    return net, tok, wrap


def padded(ids, pad_id):
    """Left-padded ids and their mask, as the model takes them."""
    pad = SEQ - len(ids)
    return [pad_id] * pad + ids, [0] * pad + [1] * len(ids)


def oracle(checkout):
    import torch
    net, tok, wrap = load_model(checkout)
    rows = load_oracle()
    start = time.time()
    with torch.no_grad():
        for i, row in enumerate(rows):
            row["wrap"] = wrap(row["hyp"])
            ids, mask = padded(row["wrap"], tok.pad_token_id)
            logits = net(torch.tensor([ids], dtype=torch.long), torch.tensor([mask], dtype=torch.long))
            row["p"] = torch.softmax(logits[0, :2].float(), dim=0)[0].item()
            if (i + 1) % 200 == 0:
                print("%d/%d %.0fs" % (i + 1, len(rows), time.time() - start), flush=True)
    save_oracle(rows)
    print("rescored %d rows, longest prompt %d tokens" % (len(rows), max(len(r["wrap"]) for r in rows)))


def doubt(conf):
    """One minus the mean word confidence, leaving out the first and last words."""
    inner = conf[1:-1] if len(conf) > 2 else conf
    return 1 - sum(inner) / len(inner)


def stats(flags, wrong):
    tp = sum(f and w for f, w in zip(flags, wrong))
    fn = sum((not f) and w for f, w in zip(flags, wrong))
    fp = sum(f and not w for f, w in zip(flags, wrong))
    recall, precision = tp / max(1, tp + fn), tp / max(1, tp + fp)
    return {"missed": fn, "wrong": tp + fn, "flagged": tp + fp, "accuracy": (len(wrong) - fn - fp) / len(wrong),
            "f2": 5 * precision * recall / max(1e-9, 4 * precision + recall)}


def fit():
    rows = load_oracle()
    first = [r for r in rows if r["set"] == "set1"]
    held = [r for r in rows if r["set"] == "set2"]
    features = lambda part: [[doubt(r["conf"]), r["p"]] for r in part]
    wrong = lambda part: [r["bad"] for r in part]
    x, y = features(first), wrong(first)
    mu = [sum(col) / len(col) for col in zip(*x)]
    sd = [(sum((v - m) ** 2 for v in col) / len(col)) ** .5 or 1 for col, m in zip(zip(*x), mu)]
    scale = lambda part: [[(v - m) / s for v, m, s in zip(row, mu, sd)] for row in part]
    w, bias = [0.0, 0.0], 0.0
    for _ in range(2000):
        gw, gb = [0.0, 0.0], 0.0
        for row, target in zip(scale(x), y):
            error = 1 / (1 + math.exp(-(bias + sum(a * v for a, v in zip(w, row))))) - target
            gb += error
            gw = [g + error * v for g, v in zip(gw, row)]
        bias -= 0.5 * gb / len(x)
        w = [a - 0.5 * g / len(x) for a, g in zip(w, gw)]
    # Summed in GateConstants.score's order, so the threshold is a score Swift reproduces exactly.
    score = lambda part: [bias + w[0] * row[0] + w[1] * row[1] for row in scale(features(part))]
    # The threshold with the best F2 on the fitted set: misses cost four times a false alarm.
    fitted = score(first)
    threshold = max(sorted(set(fitted)), key=lambda t: stats([v >= t for v in fitted], y)["f2"])
    for name, part in (("fitted", first), ("held out", held)):
        s = stats([v >= threshold for v in score(part)], wrong(part))
        print("%-8s missed %d/%d, flagged %d/%d, accuracy %.1f%%, F2 %.3f"
              % (name, s["missed"], s["wrong"], s["flagged"], len(part), 100 * s["accuracy"], s["f2"]))
    print("GateConstants(mu: %r,\n              sd: %r,\n              w: %r,\n              bias: %r,\n              threshold: %r)"
          % (mu, sd, w, bias, threshold))


def convert(checkout, out):
    """Traces the encoder with explicit attention masks and converts it to a 16-bit Core ML package.

    The network's own mask construction does not trace, so the wrapper rebuilds it: full attention
    and a 64-token sliding window, fixed length 256, left padding, and a softmax over the two option
    logits read at the last prompt token.
    """
    import numpy as np
    import torch
    import torch.nn as nn
    import coremltools as ct

    net, tok, wrap = load_model(checkout)
    model = net.model
    window, head, theta = 64, 64, 160000.0
    index = torch.arange(SEQ)
    slide = (index[:, None] - index[None, :]).abs() <= window

    def rotate_half(x):
        return torch.cat((-x[..., 32:], x[..., :32]), dim=-1)

    class Wrapper(nn.Module):
        def __init__(self):
            super().__init__()
            self.model = model
            # Rows 0 and 1 of the readout are options A (misheard) and B.
            self.register_buffer("readout", net.readout.weight[:2].detach().clone())
            self.register_buffer("slide", slide)
            self.register_buffer("inv_freq", 1.0 / (theta ** (torch.arange(0, head, 2, dtype=torch.float32) / head)))

        def forward(self, input_ids, mask):
            h = self.model.embeddings(input_ids=input_ids)
            pos = (mask.cumsum(-1) - 1).clamp(min=0)
            freqs = pos[..., None] * self.inv_freq
            emb = torch.cat((freqs, freqs), dim=-1)
            cos = emb.cos().to(h.dtype).unsqueeze(1)
            sin = emb.sin().to(h.dtype).unsqueeze(1)

            nonpad = (mask > 0.5)[:, None, None, :]
            qpad = (mask > 0.5)[:, None, :, None]
            minv = torch.finfo(torch.float32).min
            full_b = nonpad & qpad
            slide_b = self.slide[None, None, :, :] & full_b
            full_mask = torch.where(full_b, 0.0, minv).expand(1, 1, SEQ, SEQ)
            slide_mask = torch.where(slide_b, 0.0, minv)

            for layer in self.model.layers:
                am = full_mask if layer.attention_type == "full_attention" else slide_mask
                x = layer.attn_norm(h)
                qkv = layer.attn.Wqkv(x).view(1, SEQ, 3, 16, head)
                q, k, v = qkv.unbind(dim=2)
                q = q.transpose(1, 2); k = k.transpose(1, 2); v = v.transpose(1, 2)
                qf = q.float(); kf = k.float()
                q = (qf * cos + rotate_half(qf) * sin).to(h.dtype)
                k = (kf * cos + rotate_half(kf) * sin).to(h.dtype)
                aw = torch.matmul(q, k.transpose(2, 3)) * (head ** -0.5) + am
                aw = torch.softmax(aw, dim=-1, dtype=torch.float32).to(h.dtype)
                ao = torch.matmul(aw, v).transpose(1, 2).reshape(1, SEQ, 1024)
                h = h + layer.attn.Wo(ao)
                h = h + layer.mlp(layer.mlp_norm(h))
            h = self.model.final_norm(h)
            logits = nn.functional.linear(h[:, -2], self.readout)
            return torch.softmax(logits.float(), dim=-1)[:, 0]

    wrapper = Wrapper().eval()

    def inputs(row):
        ids, mask = padded(row["wrap"], tok.pad_token_id)
        return torch.tensor([ids], dtype=torch.int32), torch.tensor([mask], dtype=torch.float32)

    # The wrapper has to match the real network before it is worth converting.
    rows = load_oracle()[::400]
    for row in rows:
        with torch.no_grad():
            got = wrapper(*inputs(row)).item()
        print("wrapper %.6f oracle %.6f" % (got, row["p"]))
        if abs(got - row["p"]) > 1e-4:
            sys.exit("the traceable wrapper does not match the network")

    with torch.no_grad():
        traced = torch.jit.trace(wrapper, inputs(rows[0]), check_trace=False)
    package = ct.convert(
        traced,
        inputs=[ct.TensorType(name="input_ids", shape=(1, SEQ), dtype=np.int32),
                ct.TensorType(name="mask", shape=(1, SEQ), dtype=np.float32)],
        outputs=[ct.TensorType(name="p", dtype=np.float32)],
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.iOS18,
        compute_units=ct.ComputeUnit.ALL,
    )
    os.makedirs(out, exist_ok=True)
    path = os.path.join(out, PACKAGE)
    package.save(path)
    print("saved", path)


def parity(package):
    import numpy as np
    import coremltools as ct
    from coremltools.models import MLModel

    model = MLModel(package, compute_units=ct.ComputeUnit.ALL)
    # 50283 is the tokenizer's padding id.
    worst, start = 0.0, time.time()
    rows = load_oracle()
    for i, row in enumerate(rows):
        ids, mask = padded(row["wrap"], 50283)
        out = model.predict({"input_ids": np.array([ids], dtype=np.int32), "mask": np.array([mask], dtype=np.float32)})
        worst = max(worst, abs(float(np.array(out["p"]).reshape(-1)[0]) - row["p"]))
        if (i + 1) % 200 == 0:
            print("%d/%d worst %.4f %.0fs" % (i + 1, len(rows), worst, time.time() - start), flush=True)
    print("largest difference from the oracle: %.5f (limit 0.01)" % worst)
    if worst > 0.01:
        sys.exit(1)


def pack(package):
    if os.path.basename(os.path.normpath(package)) != PACKAGE:
        sys.exit("the package must be named " + PACKAGE)
    out = os.path.join(ROOT, "dist/models/glide-0.4b.tar")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    # A plain ustar archive, which ModelStore unpacks itself; the licence requires its files alongside.
    subprocess.run(["tar", "--format", "ustar", "--no-xattrs", "--no-mac-metadata", "-cf", out,
                    "-C", LICENSES, "LICENSE", "NOTICE", "Ettin-encoder-400m-LICENSE",
                    "-C", os.path.dirname(os.path.abspath(os.path.normpath(package))), PACKAGE],
                   check=True, env=dict(os.environ, COPYFILE_DISABLE="1"))
    digest = hashlib.sha256()
    with open(out, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            digest.update(chunk)
    print("%s\nbyteSize: %d\nsha256: %s" % (out, os.path.getsize(out), digest.hexdigest()))


COMMANDS = {"oracle": (oracle, 1), "fit": (fit, 0), "convert": (convert, 2), "parity": (parity, 1), "pack": (pack, 1)}

if __name__ == "__main__":
    command, count = COMMANDS.get(sys.argv[1] if len(sys.argv) > 1 else "", (None, 0))
    if command is None or len(sys.argv) != count + 2:
        sys.exit(__doc__)
    command(*sys.argv[2:])
