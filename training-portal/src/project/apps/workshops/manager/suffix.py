"""
Deterministic, reversible, "looks-random" suffix generator for an increasing
integer index, rendered in a base-N alphabet (default: Kubernetes' 27-char set).

- Input is your monotonically increasing integer (DB id, sequence, etc.).
- Output is a fixed-width string that looks unrelated to its neighbours.
- It is a bijection: no collisions, and you can recover the integer (with the key).
- Width starts at `min_width` and steps up by one character only when the
  current width's capacity is exhausted.

The permutation is a small balanced Feistel network with cycle-walking, keyed by
`key` (use your initial-record timestamp). This is NOT a security boundary --
it provides the *appearance* of randomness only.
"""

import functools
import hashlib
import math

DEFAULT_ALPHABET = "bcdfghjklmnpqrstvwxz2456789"  # 27 chars, no vowels, K8s-style


class SuffixCodec:
    def __init__(self, key, min_width=3, max_width=12,
                 alphabet=DEFAULT_ALPHABET, rounds=4):
        if len(set(alphabet)) != len(alphabet):
            raise ValueError("alphabet characters must be unique")
        if min_width < 1:
            raise ValueError("min_width must be >= 1")
        if max_width < min_width:
            raise ValueError("max_width must be >= min_width")

        self.key = key if isinstance(key, bytes) else str(key).encode()
        self.alphabet = alphabet
        self.base = len(alphabet)
        self.min_width = min_width
        self.max_width = max_width
        self.rounds = rounds
        self._index = {c: i for i, c in enumerate(alphabet)}

        # Per-width Feistel parameters. `bits` is the smallest *even* number with
        # 2**bits >= base**width, so the domain wraps the value range; the gap is
        # absorbed by cycle-walking.

        self._params = {}  # width -> (N, half, mask)
        for w in range(min_width, max_width + 1):
            N = self.base ** w
            bits = max(2, math.ceil(math.log2(N)))
            if bits % 2:
                bits += 1
            half = bits // 2
            self._params[w] = (N, half, (1 << half) - 1)

        self.capacity = self.base ** max_width  # number of indices representable

    # ---- base-N rendering (fixed width, leading-"zero" padded) ----

    def _to_base(self, n, width):
        out = []
        for _ in range(width):
            n, r = divmod(n, self.base)
            out.append(self.alphabet[r])
        return "".join(reversed(out))

    def _from_base(self, s):
        n = 0
        for ch in s:
            n = n * self.base + self._index[ch]
        return n

    # ---- keyed Feistel round function ----

    def _F(self, value, rnd, mask):
        h = hashlib.sha256()
        h.update(self.key)
        h.update(b":%d:%d" % (rnd, value))
        return int.from_bytes(h.digest()[:8], "big") & mask

    def _encrypt(self, x, width):
        _, half, mask = self._params[width]
        l, r = (x >> half) & mask, x & mask
        for rnd in range(self.rounds):
            l, r = r, l ^ self._F(r, rnd, mask)
        return (l << half) | r

    def _decrypt(self, y, width):
        _, half, mask = self._params[width]
        l, r = (y >> half) & mask, y & mask
        for rnd in reversed(range(self.rounds)):
            r_old = l
            l_old = r ^ self._F(r_old, rnd, mask)
            l, r = l_old, r_old
        return (l << half) | r

    # ---- cycle-walking to confine the permutation to [0, N) ----

    def _fpe_encrypt(self, x, width):
        N = self._params[width][0]
        y = self._encrypt(x, width)
        while y >= N:
            y = self._encrypt(y, width)
        return y

    def _fpe_decrypt(self, y, width):
        N = self._params[width][0]
        x = self._decrypt(y, width)
        while x >= N:
            x = self._decrypt(x, width)
        return x

    # ---- width banding ----

    def _width_for(self, i):
        for w in range(self.min_width, self.max_width + 1):
            if i < self._params[w][0]:
                return w
        raise ValueError(
            f"index {i} exceeds capacity {self.capacity} (raise max_width)")

    # ---- public API ----

    def encode(self, i):
        if i < 0:
            raise ValueError("index must be non-negative")
        w = self._width_for(i)
        return self._to_base(self._fpe_encrypt(i, w), w)

    def decode(self, suffix):
        w = len(suffix)
        if w not in self._params:
            raise ValueError("suffix length outside configured width range")
        i = self._fpe_decrypt(self._from_base(suffix), w)
        if self._width_for(i) != w:
            raise ValueError("suffix is not a valid output of this codec/key")
        return i

@functools.lru_cache(maxsize=100)
def get_suffix_codec(key, min_width=3, max_width=12,
                     alphabet=DEFAULT_ALPHABET, rounds=4):
    return SuffixCodec(key, min_width, max_width, alphabet, rounds)
