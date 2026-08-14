# AP2 v0.2 — Implementation Reference (transcribed from primary source)

**Source:** https://github.com/google-agentic-commerce/AP2
**Commit:** `e1ea56db72a6385bce3e5c1112b3a56ce60acb43`
**Commit date:** 2026-04-29T09:51:41-07:00
**Version:** v0.2 — `docs/ap2/specification.md` line 1 reads "# Agentic Payment Protocol (v0.2)"; `CHANGELOG.md` records `## 0.2.0 (2026-04-28) — Release of V2`.

Everything below is quoted verbatim or transcribed field-for-field from that commit.
Quoted text uses `>` blockquotes with a file:line citation. Anything that is *not*
from the spec is explicitly marked **[NOT IN SPEC]** or lives in §12 (Gaps).

> **Reading note on the docs.** `docs/ap2/checkout_mandate.md` and
> `docs/ap2/payment_mandate.md` do not contain field tables in source form. They
> contain mkdocs-macro calls like `{{ schema_fields('checkout_mandate', 'ap2',
> show_sd=True) }}` which are rendered at build time from the JSON Schemas in
> `code/sdk/schemas/`. **The JSON Schemas are therefore the normative field
> definitions**, and all field tables below are transcribed from them.

---

## 1. The five roles

From `docs/ap2/specification.md:30-55`.

| Role | Abbrev | Produces | Verifies |
| --- | --- | --- | --- |
| Shopping Agent | SA | Closed Checkout + Payment Mandates (agent-signed, autonomous mode); assembles all Mandate Content | Receipts it receives |
| Credential Provider | CP | Payment credential / token | Payment Mandate |
| Merchant | M | **Checkout JWT**, Checkout Receipt | Checkout Mandate |
| Merchant Payment Processor | MPP | Payment Receipt | Payment Mandate (inside the payment credential), checkout binding |
| Trusted Surface | TS | User-signed Checkout + Payment Mandates (open or closed) | — (obtains user consent) |

Role constraints:

> The following role MUST be non-agentic:
> -   Trusted Surface
> — `docs/ap2/specification.md:78-80`

> When this document refers to validation or processing for a particular role, it
> MUST happen in deterministic code regardless of whether the role is agentic or
> not.
> — `docs/ap2/specification.md:96-98`

> Roles MAY always delegate their responsibilities to another party.
> — `docs/ap2/specification.md:57`

Merchant, MPP and CP MAY be agentic or non-agentic; SA "is expected to be agentic"
(`specification.md:72-84`). A single entity MAY play multiple or all roles
(`specification.md:53-55`).

---

## 2. The four `vct` strings (confirmed)

| Artifact | `vct` (exact) | Source |
| --- | --- | --- |
| Closed Checkout Mandate | `mandate.checkout.1` | `docs/ap2/checkout_mandate.md:14`, `code/sdk/schemas/ap2/checkout_mandate.json` (`const`) |
| Open Checkout Mandate | `mandate.checkout.open.1` | `docs/ap2/checkout_mandate.md:15`, `code/sdk/schemas/ap2/open_checkout_mandate.json` (`const`) |
| Closed Payment Mandate | `mandate.payment.1` | `docs/ap2/payment_mandate.md:14`, `code/sdk/schemas/ap2/payment_mandate.json` (`const`) |
| Open Payment Mandate | `mandate.payment.open.1` | `docs/ap2/payment_mandate.md:15`, `code/sdk/schemas/ap2/open_payment_mandate.json` (`const`) |

> A closed Checkout Mandate MUST use the value `mandate.checkout.1` for the `vct`
> claim and an open Checkout Mandate MUST use the value `mandate.checkout.open.1`.
> — `docs/ap2/checkout_mandate.md:14-15`

> A closed Payment Mandate MUST use the value `mandate.payment.1` for the `vct`
> claim, and an open Payment Mandate MUST use the value `mandate.payment.open.1`.
> — `docs/ap2/payment_mandate.md:14-15`

Versioning rule (verbatim):

> Each AP2 Mandate type identifies its schema using the `vct` claim. The `vct`
> value includes a numeric suffix that acts as a schema version number (e.g.
> `mandate.payment.1`, `mandate.checkout.open.1`). Implementations MUST match the
> exact `vct` string, including the version suffix. A future incompatible schema
> revision would introduce a new suffix (e.g. `.2`), allowing old and new versions
> to be distinguished unambiguously.
> — `docs/ap2/specification.md:138-143`

⚠️ `code/sdk/python/ap2/sdk/README.md:113-118` has a table listing the `vct`s
**without** the `.1` suffix (`mandate.payment.open`, `mandate.checkout`, …). That
table is informal/stale — the generated Pydantic models
(`code/sdk/python/ap2/sdk/generated/*.py`) all use `Literal['mandate.…​.1']`.
The schema `const` values with `.1` are authoritative. Note also that the JSON
Schema `description` strings themselves say e.g. `"MUST be 'mandate.checkout'"`
while the adjacent `const` says `"mandate.checkout.1"` — the `const` wins.

---

## 3. Checkout JWT (merchant-signed)

### 3.1 What it is

> The The Merchant MUST provide a merchant-signed JWT containing the Checkout to
> the Shopping Agent. The closed Checkout Mandate is bound to this Checkout JWT
> using a cryptographic hash.
> — `docs/ap2/specification.md:126-128`

It is a **plain compact JWS**, not an SD-JWT.

> `checkout_jwt` is the merchant-signed JWT containing the details of the
> checkout. The details of the payload are outside the scope of this
> specification, when used with the [Universal Commerce Protocol](https://ucp.dev)
> this MUST be the Checkout object.
> — `docs/ap2/checkout_mandate.md:30-33`

AP2 is deliberately payload-agnostic here:

> AP2 is agnostic to the contents of the merchant-signed Checkout JWT. It is
> created to be compatible with logically represented Checkout Objects, but it
> does provide an extension point to be adapted to other Checkout Objects.
> — `docs/ap2/specification.md:383-386`

### 3.2 Signature algorithm requirement (verbatim — this is the ECDSA-not-Ed25519 rule)

> The Payment Mandate is bound to a particular Checkout using the cryptographic
> hash of the Checkout JWT. To prevent rainbow table attacks, the Checkout JWT
> MUST be signed using a digital signature scheme (e.g., ECDSA) and not a
> deterministic signature (e.g., Ed25519).
> — `docs/ap2/specification.md:154-157`

And the accompanying rationale + escape hatch, verbatim:

> The `checkout_hash` makes use of the entropy already included in the JWT
> signature to prevent guessing the Checkout contents. If a signing algorithm
> (e.g. deterministic signature scheme such as `Ed25519`) is used that does not
> include this then a salt of sufficient entropy MUST be present in the Checkout.
> — `docs/ap2/security_and_privacy_considerations.md` (§Rainbow Table Attacks, last paragraph)

**Reading:** the hash is *not* salted separately. The security of `checkout_hash`
against dictionary/rainbow attack rests on the randomness of the ECDSA `k` value
appearing in the signature bytes, which are part of the hashed input. A
deterministic scheme removes that entropy — hence the prohibition. If you use one
anyway, you MUST put an explicit high-entropy salt inside the Checkout payload.

### 3.3 Structure of the Checkout object (UCP)

From `code/sdk/schemas/ucp/types/checkout.json` — *"UCP Checkout object
(dev.ucp.shopping.checkout 2026-04-08). The merchant field is an AP2 extension for
mandate binding."* `additionalProperties: true`.

Required: `id`, `line_items`, `status`, `currency`, `totals`, `links`.

| Field | Type | Req | Notes |
| --- | --- | --- | --- |
| `id` | string | ✅ | Unique identifier of the checkout session |
| `merchant` | `types/merchant.json` | ❌ | **AP2 extension** for mandate binding |
| `line_items` | array of `line_item.json` | ✅ | |
| `status` | enum | ✅ | `incomplete`, `requires_escalation`, `ready_for_complete`, `complete_in_progress`, `completed`, `canceled` |
| `currency` | string | ✅ | ISO-4217 alpha-3 |
| `totals` | array of `total.json` | ✅ | "Must contain exactly one subtotal and one total entry" |
| `links` | array of `link.json` | ✅ | |
| `buyer` | `buyer.json` | ❌ | |
| `messages` | array of `message.json` | ❌ | |
| `expires_at` | string (date-time) | ❌ | |
| `continue_url` | string (uri) | ❌ | |

**Amounts are integer minor units.** `ap2/types/item.json` → `price`: *"Unit price
in the currency's minor unit as defined by ISO 4217"*, `type: integer, minimum: 0`.

### 3.4 How the sample merchant builds it **[NOT IN SPEC — sample code]**

`code/samples/python/src/roles/merchant_agent/tools.py:205-207`:

```python
header = {"alg": "ES256", "typ": "JWT", "kid": "merchant-key-1"}
checkout_jwt = _create_jwt(header, checkout_payload, merchant_key)
checkout_hash = _compute_sha256_b64url(checkout_jwt)
```

The payload is `checkout.model_dump(mode="json", exclude_none=True)` plus `iat` and
`exp` (`now + 3600`) added at the top level of the Checkout object (`tools.py:201-204`).
The spec does not require `iat`/`exp` on the Checkout JWT — that is a sample choice.

---

## 4. THE HASH BINDING MECHANISM (the load-bearing part)

There are **five distinct hashes** in AP2 v0.2. Getting them confused is the most
likely source of a non-interoperable implementation, so each is nailed down here.

### 4.1 `checkout_hash` — binds closed Checkout Mandate → Checkout JWT

Normative definition, verbatim:

> `checkout_hash` is the base64url-encoded hash of the value of `checkout_jwt`.
> The algorithm used MUST be the same as the SD-JWT, as defined by the `_sd_alg`
> claim in the base payload, or `sha-256` if not present.
> — `docs/ap2/checkout_mandate.md:26-28`

Schema restatement (`code/sdk/schemas/ap2/checkout_mandate.json`):

> "base64url-encoded hash of the checkout_jwt field value, uniquely identifying
> this checkout. If this checkout mandate is presented as an sd-jwt and the
> _sd_alg field is present then the hash algorithm used MUST match the _sd_alg
> field. Otherwise, sha-256 MUST be used."

**Over what bytes:** the *value of the `checkout_jwt` claim*, i.e. **the JWS compact
serialization string** `base64url(header).base64url(payload).base64url(signature)`
— **not** the decoded payload, and not the payload JSON. Confirmed by the reference
implementation, `code/samples/python/src/roles/merchant_agent/tools.py:88-89, 207`:

```python
def _compute_sha256_b64url(data: str) -> str:
  return _b64url_encode(hashlib.sha256(data.encode()).digest())
...
checkout_hash = _compute_sha256_b64url(checkout_jwt)   # checkout_jwt is the compact string
```

And `code/sdk/python/ap2/sdk/utils.py:21-36`:

```python
def b64url_encode(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b'=').decode('ascii')   # PADDING STRIPPED

def compute_sha256_b64url(data: str) -> str:
    return b64url_encode(hashlib.sha256(data.encode()).digest())
```

So: **`checkout_hash = b64url_nopad(SHA-256(ascii_bytes_of_compact_checkout_jwt)))`**.

Consistency requirement, verbatim:

> When calculating hashes it is important that the same representation is used.
> This is typically achieved by providing the base64url encoded representation of
> JSON structures. For dispute resolution this will mean storing the SD-JWTs,
> along with their disclosures, for the Mandates in their compact serialization.
> This is to allow easy computation of the `sd_hash`, `checkout_hash`, and Receipt
> `reference`.
>
> For consistency, the same hashing algorithm is required for both the SD-JWT
> digests and `checkout_hash`.
> — `docs/ap2/implementation_considerations.md` (§Hashes)

### 4.2 `transaction_id` — binds closed Payment Mandate → the same Checkout

`code/sdk/schemas/ap2/payment_mandate.json`:

> "base64url-encoded hash of the checkout_jwt field value, uniquely identifying
> the checkout associated with this. The hash algorithm used MUST be the same as
> the sd_hash field for this sd-jwt, or sha256 if absent."

**`transaction_id` carries the identical value as `checkout_hash`.** Confirmed by
the worked example: `docs/ap2/checkout_mandate.md:317` has
`"checkout_hash": "NivWhuqfzcvZNapvIEJ2-3tsdQLkiuIcye2g46WVgX8"` and
`docs/ap2/payment_mandate.md:383` has
`"transaction_id": "NivWhuqfzcvZNapvIEJ2-3tsdQLkiuIcye2g46WVgX8"` — the same string.
Also `code/samples/python/src/roles/shopping_agent/tools.py:201-209` passes
`transaction_id=checkout_hash`.

Security rationale, verbatim:

> - The Payment Mandate MUST contain a reference to its associated Checkout.
> - This is via `transaction_id` for closed Payment Mandates and the
>  `mandate.payment.reference` constraint for open ones.
> — `docs/ap2/security_and_privacy_considerations.md` (§Manipulated Checkout)

(The constraint type is actually `payment.reference`, not `mandate.payment.reference` —
see §12.10.)

### 4.3 `sd_hash` — binds closed Mandate → the open Mandate it was issued under

This is standard SD-JWT key-binding. Definition from the implementation,
`code/sdk/python/ap2/sdk/sdjwt/common.py:54-57, 165-172`:

```python
@property
def sd_jwt(self) -> str:
    if self.disclosures:
        return self.issuer_jwt + '~' + '~'.join(self.disclosures) + '~'
    return self.issuer_jwt + '~'

def _hash_ascii(value: str, sd_alg: str | None) -> str:
    digest = _hash_for_alg(sd_alg)(value.encode('ascii')).digest()
    return b64url_encode(digest)

def compute_sd_hash(token: ParsedToken) -> str:
    """Hash an SD-JWT including disclosures, excluding a trailing KB-JWT."""
    return _hash_ascii(token.sd_jwt, token.sd_alg)
```

So **`sd_hash` covers the preceding hop's issuer JWT *plus its disclosures*, with a
trailing `~`, and excludes any trailing KB-JWT.** Supported `_sd_alg` values are
`sha-256`, `sha-384`, `sha-512` (`common.py:24-28`); absent ⇒ `sha-256`.

Normative security requirement, verbatim:

> - Closed Mandates MUST contain the `sd_hash` claim to bind them to the
>   presented open Mandate.
> - Open Mandates MUST contain the Agent's key (via a `cnf` claim) so that
>   only the agent could create a Closed Mandate with a valid signature.
> — `docs/ap2/security_and_privacy_considerations.md` (§Manipulated Checkout)

**SDK-only alternative — `issuer_jwt_hash` [NOT IN THE AP2 DOCS]:** the SDK also
supports binding via `issuer_jwt_hash` = hash of *only* the preceding issuer JWT
(no disclosures), letting a downstream delegate redact disclosures without breaking
the chain (`common.py:175-177`, `sdk/README.md:59-71`). `verify_binding` requires
**exactly one** of `sd_hash` / `issuer_jwt_hash` (`common.py:198-207`). The AP2
specification only ever mentions `sd_hash`. Treat `issuer_jwt_hash` as a
non-conformant extension unless you need it.

### 4.4 `conditional_transaction_id` — binds open Payment Mandate → open Checkout Mandate

Schema (`open_payment_mandate.json` → `$defs/payment_reference`): *"Digest of the
associated Open Checkout Mandate."*

Evaluation rule, verbatim:

> The Checkout Mandate for the approved order MUST contain an open Checkout
> Mandate with a matching hash in its delegate chain. The hash algorithm used
> MUST be the `_sd_alg` algorithm for the SD-JWT this constraint is in, or
> `sha-256` if undefined.
> — `docs/ap2/payment_mandate.md:230-234`

Computed as an `sd_hash`-style digest over the open Checkout Mandate token.
Sample: `code/samples/python/src/roles/merchant_agent_mcp/server.py:781`

```python
open_checkout_hash = compute_sd_hash(parse_token(open_checkout_mandate))
```

Worked example cross-check: the open Payment Mandate's
`conditional_transaction_id` is `FzLoxbbtgQGYZxoSM2NJYJtkFTSsdfUBoVEQ12k7JN8`
(`payment_mandate.md:328`), which is the same value as the closed Checkout
Mandate's `sd_hash` (`checkout_mandate.md:266`).

### 4.5 Receipt `reference` — binds Receipt → closed Mandate

Normative, verbatim:

> - *reference*: **REQUIRED**. A String value that is the base64url-encoded
>   hash of the received Mandate. When receiving a chain of Mandates, it is
>   a hash over the final SD-JWT in the chain. It is calculated in the same
>   manner as `sd_hash`. The algorithm used MUST be the same as the
>   `_sd_alg` specified for the SD-JWT, or `sha-256` if not specified.
> — `docs/ap2/agent_authorization.md:509-513`

And in the dispute rules:

> -   The Checkout Receipt `reference` MUST match the hash of the closed Checkout
>     Mandate. This is calculated in the same manner as the `sd_hash` would be.
> -   The Payment Receipt reference MUST match the hash of the closed Payment
>     Mandate. This is calculated in the same manner as the `sd_hash` would be.
> — `docs/ap2/specification.md:354-360`

⚠️ **The reference SDK does NOT do this.** See §12.1 — this is the single biggest
divergence found. The SDK computes `sha256(leaf issuer JWT only)`, which is the
`issuer_jwt_hash` manner, not the `sd_hash` manner.

### 4.6 ✅ VERIFIED against the spec's own encoded test vectors

I decoded the base64url tokens in `docs/ap2/checkout_mandate.md` and recomputed
every hash. **All three binding rules above are confirmed cryptographically** —
these are not inferences, they reproduce exactly:

| Check | Computed | Matches |
| --- | --- | --- |
| `b64url_nopad(SHA-256(checkout_jwt_compact_string))` | `NivWhuqfzcvZNapvIEJ2-3tsdQLkiuIcye2g46WVgX8` | = `checkout_hash` in the closed Mandate ✅ and = `transaction_id` in the closed Payment Mandate ✅ |
| `b64url_nopad(SHA-256(open_checkout_token_incl_trailing_~))` | `FzLoxbbtgQGYZxoSM2NJYJtkFTSsdfUBoVEQ12k7JN8` | = `sd_hash` in the closed Mandate ✅ and = `conditional_transaction_id` in the open Payment Mandate ✅ |
| `b64url_nopad(SHA-256(checkout_jwt_disclosure_string))` | `3A9UyZJofw2eMP-Lx2tYaNpCcuB8elnhwwLhZLwqQFM` | = the `_sd` entry inside the closed Mandate Content ✅ |

This settles the two questions that mattered most:

1. **`checkout_hash` is over the JWS compact serialization string** (the full
   `header.payload.signature`, ASCII), **not** the payload and not a re-encoding.
2. **`conditional_transaction_id` really is the `sd_hash`-style digest of the open
   Checkout Mandate**, and it is the *same value* the closed Checkout Mandate uses
   as its `sd_hash`. One digest, two claims, two documents.

Reproduce with:

```python
import base64, hashlib
b64e = lambda b: base64.urlsafe_b64encode(b).rstrip(b'=').decode()
checkout_hash = b64e(hashlib.sha256(checkout_jwt.encode('ascii')).digest())
sd_hash       = b64e(hashlib.sha256(open_token_ending_in_tilde.encode('ascii')).digest())
```

⚠️ **But the pretty-printed JSON in the docs does not match its own encoded
tokens** — see §12.16. Use the encoded tokens as test vectors, never the JSON blocks.

### 4.7 Summary diagram of the binding graph

```
                       Checkout JWT  (merchant-signed, ECDSA, compact JWS)
                             │
             checkout_hash = b64url(SHA-256(compact_jwt_string))
                             │
          ┌──────────────────┴───────────────────┐
          ▼                                      ▼
  Closed Checkout Mandate               Closed Payment Mandate
  vct=mandate.checkout.1                vct=mandate.payment.1
  checkout_hash: <H>                    transaction_id: <H>   ← same value
  checkout_jwt:  <the JWT>  (SD field)
          │                                      │
          │ sd_hash (KB-SD-JWT payload)          │ sd_hash
          ▼                                      ▼
  Open Checkout Mandate  ◄────────────  Open Payment Mandate
  vct=mandate.checkout.open.1           vct=mandate.payment.open.1
  cnf.jwk = agent key                   constraint payment.reference:
                                          conditional_transaction_id
                                          = sd_hash(open Checkout Mandate)

  Receipts: reference = hash of the closed Mandate ("same manner as sd_hash")
```

---

## 5. Checkout Mandate

### 5.1 Closed — `mandate.checkout.1`

`code/sdk/schemas/ap2/checkout_mandate.json`
*"Agreement from a User or an Agent to authorize a particular Checkout action."*

**Required: `vct`, `checkout_jwt`, `checkout_hash`.**

| Claim | Type | Req | SD | Description (verbatim from schema) |
| --- | --- | --- | --- | --- |
| `vct` | string | ✅ | | `const: "mandate.checkout.1"` |
| `checkout_jwt` | string | ✅ | **`x-selectively-disclosable-field: true`** | "base64url-encoded serialized merchant-signed JWT of the Checkout payload." |
| `checkout_hash` | string | ✅ | | see §4.1 |
| `iat` | integer | ❌ | | "The creation timestamp as a Unix epoch." |
| `exp` | integer | ❌ | | "The expiration timestamp as a Unix epoch." |

`checkout_jwt` being selectively disclosable is what lets the SA hand the *Payment*
side a Checkout Mandate whose full checkout contents are withheld, while
`checkout_hash` still links them for dispute (see §11 privacy).

**Who signs.** Human Present: the Trusted Surface with `user_sk`
(`flows.md:57-61`). Human Not Present: the Shopping Agent with `agent_sk`
(`flows.md:146-150`), where `agent_sk` corresponds to the `cnf.jwk` endorsed in the
open Checkout Mandate. Key type is EC P-256 / ES256 (§9).

**Wire example** (`docs/ap2/checkout_mandate.md:250-322`), header + payload of the
closed hop:

```json
"header": { "alg": "ES256", "typ": "kb+sd-jwt" },
"payload": {
  "delegate_payload": [ { "...": "7VLY-eKTFSShLoZRXY5jXcD2UHm1JvPmoANYRqqxy34" } ],
  "iat": 1777342376,
  "aud": "merchant",
  "nonce": "b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4",
  "sd_hash": "FzLoxbbtgQGYZxoSM2NJYJtkFTSsdfUBoVEQ12k7JN8",
  "_sd_alg": "sha-256"
}
```

…whose disclosed `delegate_payload[0]` is the Mandate Content proper:

```json
{
  "_sd": [ "3A9UyZJofw2eMP-Lx2tYaNpCcuB8elnhwwLhZLwqFFM" ],
  "vct": "mandate.checkout.1",
  "checkout_hash": "NivWhuqfzcvZNapvIEJ2-3tsdQLkiuIcye2g46WVgX8"
}
```

Note the Mandate Content sits inside a **`delegate_payload` array** in the JWT
payload, and `checkout_jwt` is hidden behind the `_sd` digest — that is the
selective-disclosure field in action.

### 5.2 Open — `mandate.checkout.open.1`

`code/sdk/schemas/ap2/open_checkout_mandate.json`
*"Agreement between a user and an agent (or chain of agents) to authorize future
checkout actions."*

**Required: `vct`, `constraints`, `cnf`.**
**`constraints` MUST contain a `checkout.line_items` entry** — the schema has
`"contains": {"$ref": "#/$defs/line_items"}`.

| Claim | Type | Req | Description |
| --- | --- | --- | --- |
| `vct` | string | ✅ | `const: "mandate.checkout.open.1"` |
| `constraints` | array | ✅ | items `anyOf` [`allowed_merchants`, `line_items`]; MUST contain ≥1 `line_items` |
| `cnf` | object | ✅ | "Confirmation claim defined in RFC 7800 section 3.1. Used for key binding." |
| `iat` | integer | ❌ | Unix epoch |
| `exp` | integer | ❌ | Unix epoch |

### 5.3 Checkout Mandate constraints

**`checkout.allowed_merchants`**

| Prop | Type | Req | SD |
| --- | --- | --- | --- |
| `type` | string, `const: "checkout.allowed_merchants"` | ✅ | |
| `allowed` | array of `types/merchant.json` | ✅ | **`x-selectively-disclosable-array: true`** |

> **Evaluation**: The Merchant MUST be present in the revealed elements of
> `allowed`. If they are not present, or if the `allowed`
> contains no revealed elements, the constraint is invalid.
> — `docs/ap2/checkout_mandate.md:56-58`

**`checkout.line_items`**

| Prop | Type | Req |
| --- | --- | --- |
| `type` | string, `const: "checkout.line_items"` | ✅ |
| `items` | array of `line_item_requirements`, `minItems: 1` | ✅ |

`line_item_requirements` (required `id`, `acceptable_items`, `quantity`):

| Prop | Type | Req | SD |
| --- | --- | --- | --- |
| `id` | string | ✅ | |
| `acceptable_items` | array of `item` | ✅ | **`x-selectively-disclosable-array: true`** |
| `quantity` | integer, `exclusiveMinimum: 0` | ✅ | |

`item` (in `open_checkout_mandate.json#/$defs/item`; required `id`, `title`) —
note this is a **narrower** shape than `ap2/types/item.json`, which additionally
requires `price`:

| Prop | Type | Req |
| --- | --- | --- |
| `id` | string | ✅ | "Unique identifier for the line item. Will often be the SKU." |
| `title` | string | ✅ |

Evaluation, verbatim:

> **Evaluation**: This constraint is met when:
>
> -   Each `items` entry in the constraint has a total quantity of matching items
>     in the Checkout.
> -   An item matches an `items` entry if its ID is present in the revealed
>     `acceptable_items`.
> -   No `items` entry or item in the Checkout may be used more than once.
> — `docs/ap2/checkout_mandate.md:83-89`

The spec then gives a maximal-flow construction (`checkout_mandate.md:91-105`):
source → node per `items` entry (capacity = quantity); node per Checkout item ID →
sink (capacity = total quantity of that ID); infinite-capacity edges between an
`items` node and each matching Checkout item node.

> The constraint is met if the maximal flow equals the total constraint `items`
> quantity and the total checkout `items` quantity.
> — `docs/ap2/checkout_mandate.md:104-105`

> NOTE: This evaluation does not support splitting the open Checkout Mandate
> across multiple Checkouts.
> — `docs/ap2/checkout_mandate.md:107-108`

(Implementation lives in `code/sdk/python/ap2/sdk/max_flow_helper.py`.)

---

## 6. Payment Mandate

### 6.1 Closed — `mandate.payment.1`

`code/sdk/schemas/ap2/payment_mandate.json`

**Required: `vct`, `transaction_id`, `payee`, `payment_amount`, `payment_instrument`.**

| Claim | Type | Req | Description |
| --- | --- | --- | --- |
| `vct` | string | ✅ | `const: "mandate.payment.1"` |
| `transaction_id` | string | ✅ | = `checkout_hash` (§4.2) |
| `payee` | `types/merchant.json` | ✅ | "The merchant receiving the payment." |
| `payment_amount` | `types/amount.json` | ✅ | "…currency (ISO 4217 code…) and amount (integer minor units per ISO 4217, e.g., 27999 = $279.99). Final value confirmed by the user." |
| `payment_instrument` | `types/payment_instrument.json` | ✅ | |
| `pisp` | `types/pisp.json` | ❌ | Payment Initiation Service Provider |
| `execution_date` | string | ❌ | "ISO8601 date of execution of payment. When absent indicates immediate execution." |
| `risk_data` | object | ❌ | "An map of relevant risk signals collected by the trusted surface at time of mandate creation." |
| `iat` | integer | ❌ | |
| `exp` | integer | ❌ | |

No `x-selectively-disclosable-*` annotations on the closed Payment Mandate.

**Wire example** (`docs/ap2/payment_mandate.md:356-402`) — closed hop header/payload:

```json
"header": { "alg": "ES256", "typ": "kb+sd-jwt" },
"payload": {
  "delegate_payload": [ { "...": "G2DuU6IjyDkD-9ItStdsUo48C5uJqDs1E9Hf5GT3TgM" } ],
  "iat": 1777342370,
  "aud": "credential-provider",
  "nonce": "a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3",
  "sd_hash": "uixoHemmfrrCSbPREo9j-ziLuMkqExsPeWrwA-PK0Ck",
  "_sd_alg": "sha-256"
}
```

Mandate Content:

```json
{
  "vct": "mandate.payment.1",
  "transaction_id": "NivWhuqfzcvZNapvIEJ2-3tsdQLkiuIcye2g46WVgX8",
  "payee": { "id": "merchant_1", "name": "Demo Merchant", "website": "https://demo-merchant.example" },
  "payment_amount": { "amount": 19900, "currency": "USD" },
  "payment_instrument": { "id": "stub", "type": "card", "description": "Card ••••4242" }
}
```

Note `aud` is `"credential-provider"` here vs `"merchant"` for the Checkout
Mandate — the two closed mandates are presented to different verifiers.

### 6.2 Open — `mandate.payment.open.1`

`code/sdk/schemas/ap2/open_payment_mandate.json`

**Required: `vct`, `constraints`, `cnf`.**
**`constraints` MUST contain a `payment.reference` entry** —
`"contains": {"$ref": "#/$defs/payment_reference"}`. Note `items` uses `oneOf`
here (vs `anyOf` in the open Checkout Mandate).

> The open Payment Mandate MAY optionally include any property from the closed
> Payment Mandate.
> — `docs/ap2/payment_mandate.md:26-27`

The schema materialises that as explicit optional properties: `payee`,
`payment_amount`, `payment_instrument`, `pisp`, `execution_date`, `risk_data`,
`iat`, `exp` — plus `cnf` (required). `payment_amount` here is described as
*"Pre-set by the user at time of mandate creation"* (vs "Final value confirmed by
the user" in the closed one).

### 6.3 Payment Mandate constraints (all eight)

| Type string | Required props | Optional props |
| --- | --- | --- |
| `payment.agent_recurrence` | `type`, `frequency` | `max_occurrences` (integer) |
| `payment.allowed_payees` | `type`, `allowed` | — |
| `payment.allowed_payment_instruments` | `type`, `allowed` | — |
| `payment.allowed_pisps` | `type`, `allowed` | — |
| `payment.amount_range` | `type`, `currency`, `max` | `min` |
| `payment.budget` | `type`, `max`, `currency` | — |
| `payment.execution_date` | `type` | `not_before`, `not_after` |
| `payment.reference` | `type`, `conditional_transaction_id` | — |

Selective disclosure: `allowed` is `x-selectively-disclosable-array: true` on
`allowed_payees` and `allowed_payment_instruments`. **It is NOT** on
`allowed_pisps` (an asymmetry; possibly unintentional).

`frequency` enum (exact): `ON_DEMAND`, `DAILY`, `WEEKLY`, `BIWEEKLY`, `MONTHLY`,
`QUARTERLY`, `ANNUALLY`.

Types: `amount_range.max`/`min` are **`integer`** ("Maximum allowed amount in minor
(cents) unit of currency"). `budget.max` is **`number`**. See §12.6.

Evaluation rules, verbatim:

- **agent_recurrence** — > This constraint evaluates as true if the current Payment Mandate is sufficiently separated in time from the previous presentation to meet the `frequency` definition, and the `max_occurrences` limit is greater than or equal to the current occurrences. (`payment_mandate.md:57-62`)
- **allowed_payees** — > The `payee` property of the Payment Mandate MUST be present in the `allowed` array. (`payment_mandate.md:85-87`)
- **allowed_payment_instruments** — > The `payment_instrument` property of the Payment Mandate MUST be present in the `allowed` array. (`payment_mandate.md:115-117`)
- **allowed_pisps** — > The PISP facilitating the transaction MUST be present in the `allowed` array. (`payment_mandate.md:149-150`)
- **amount_range** — > The `payment_amount` property of the Payment Mandate MUST be within the range defined by `min` and `max`. The `currency` property of the Payment Mandate MUST match the `currency` property of this constraint. (`payment_mandate.md:178-180`)
- **budget** — > the requested amount plus the total sum of amounts from previously closed Payment Mandates MUST be less than or equal to `max`. After approval, the amount MUST be added to the accumulated total for future evaluation. (`payment_mandate.md:205-208`)
- **reference** — see §4.4.
- **execution_date** — > The `execution_date` of the Payment Mandate MUST be later than or equal to `not_before` (if present) and earlier than or equal to `not_after` (if present). (`payment_mandate.md:255-257`)

---

## 7. Receipts

### 7.1 Generic Mandate Receipt (agent authorization layer)

Verbatim, `docs/ap2/agent_authorization.md:496-519`:

> Upon acceptance or rejection of the Mandate, the Verifier MUST return a signed
> Mandate Receipt.
> Upon receipt of a successful Mandate Receipt, the Agent stores the
> open Mandate-closed Mandate-Mandate Receipt tuple. The agent reduces the scope
> of the open mandate based on the receipt, often preventing future presentations
> entirely.
>
> A Mandate Receipt is a Verifier-signed JWT with the following properties:
>
>   - *iss*: **REQUIRED**. A String containing the issuer of the JWT, which
>     MUST be the Verifier.
>   - *result*: **REQUIRED**. An Enum with value `["success", "error"]`
>     indicating the result of the action authorization.
>   - *reference*: **REQUIRED**. […see §4.5…]
>   - *error*: **OPTIONAL**. A String error code identifying the error. MUST be
>     present when the result is `"error"`.
>   - *error_description*: **OPTIONAL**. A human-readable error description String.
>
> It MAY contain additional use-case specific properties, based on the action that
> was authorized, and the Mandate Type received.

⚠️ The concrete AP2 receipt schemas use **`status`** with enum
**`["Success", "Error"]`** (capitalised), not `result` with `["success","error"]`.
See §12.2. **Implement `status`** — that is what the schemas, the generated models,
and the samples all use.

### 7.2 Checkout Receipt

`code/sdk/schemas/ap2/checkout_receipt.json` — *"Receipt that supplies information
about the final state of a checkout."* **Signed by the Merchant.**

**Required: `status`, `iss`, `iat`, `reference`.**
Plus a `oneOf` discriminator:
- `status == "Success"` ⇒ **`order_id` REQUIRED**
- `status == "Error"` ⇒ **`error` AND `error_description` REQUIRED**

| Field | Type | Req | Description |
| --- | --- | --- | --- |
| `status` | `types/receipt_status.json` → enum `["Success","Error"]` | ✅ | "The status of the checkout." |
| `iss` | string | ✅ | "The issuer of the receipt." |
| `iat` | integer | ✅ | Unix epoch |
| `reference` | string | ✅ | "The hash of the closed Mandate that this receipt is binding to." |
| `order_id` | string | cond. | "Present if and only if status is Success." |
| `error` | string | cond. | "Present if and only if status is Error." |
| `error_description` | string | cond. | "Present if and only if status is Error." |

Obligation, verbatim:

> Once the Merchant has accepted or rejected the Checkout Mandate, it MUST return
> a Checkout Receipt.
> — `docs/ap2/specification.md:130-131`

> If any step fails, the Merchant MUST return a Checkout Receipt JWT containing
> the appropriate error message.
> — `docs/ap2/specification.md:316-317`

### 7.3 Payment Receipt

`code/sdk/schemas/ap2/payment_receipt.json` — **signed by the Merchant Payment
Processor** (and by the CP/Network on their own verification failures).

**Required: `status`, `iss`, `iat`, `reference`, `payment_id`.**
Plus `oneOf`:
- `status == "Success"` ⇒ **`psp_confirmation_id` AND `network_confirmation_id` REQUIRED**
- `status == "Error"` ⇒ **`error` AND `error_description` REQUIRED**

| Field | Type | Req | Description |
| --- | --- | --- | --- |
| `status` | enum `["Success","Error"]` | ✅ | |
| `iss` | string | ✅ | |
| `iat` | integer | ✅ | |
| `reference` | string | ✅ | hash of closed Mandate |
| `payment_id` | string | ✅ | "A unique identifier for the payment." |
| `psp_confirmation_id` | string | cond. | "Present only if status is Success." |
| `network_confirmation_id` | string | cond. | "Present only if status is Success." |
| `error` / `error_description` | string | cond. | Present iff Error |

Note `payment_id` is required **unconditionally**, including on Error receipts.

Obligation, verbatim:

> Once the Merchant Payment Processor has accepted or rejected the Payment
> Mandate, a signed Payment Receipt MUST be returned to the Shopping Agent,
> Credential Provider, and possibly Networks.
> — `docs/ap2/specification.md:159-161`

> If any step fails, they MUST return a Payment Receipt JWT containing the
> appropriate error to the Shopping Agent.
> — `docs/ap2/specification.md:332-333`

**Receipt JWT header [NOT IN SPEC]:** the spec never states the receipt JWT's
`alg`/`typ`. The SDK signs receipts with plain ES256 via
`code/sdk/python/ap2/sdk/jwt_helper.py` (`create_jwt(header, payload, key)` with
`alg='ES256'`). Sample `iss` values: merchant website for Checkout Receipts;
`payment_mandate_content.pisp.domain_name` (or `''` if no PISP) for Payment
Receipts (`receipt_wrapper.py`).

---

## 8. Error / rejection semantics

The complete normative error list, verbatim from `docs/ap2/agent_authorization.md:521-535`:

> ### Errors
> The following errors are defined for all action authorizations:
>
>   - `invalid_credential`: Returned when the Mandate fails verification. This
>     represents a terminal error.
>   - `unresolved_constraint`: Returned when the Mandate contains an unknown
>     constraint, or the Verifier is unable to verify that the closed Mandate conforms to the
>     provided constraints. This MAY be used as a signal to fallback
>     to either a directly approved closed Mandate, or other non-agentic
>     flows.
>   - `invalid_mandate`: Returned when the provided Mandate fails to approve the
>     requested action. This represents a terminal error.
>   - `mandates_not_supported`: Indicates that the Verifier does not support
>     mandates for approving this action. This MAY be used as a signal to fallback
>     to non-agentic flows.

Fallback semantics:

> A Human Not Present flow can be turned into a Human Present flow by the Merchant
> (or Credential Provider) returning an `unresolved_constraint` error and
> bringing the User back into the loop to approve the closed Mandates.
> — `docs/ap2/flows.md:11-13`

Unknown-constraint rule (this is a fail-closed requirement):

> - Any unknown Constraints MUST be treated as failing evaluation.
> — `docs/ap2/agent_authorization.md:465`

---

## 9. Key distribution — what the spec actually says (very little)

**This is a genuine gap.** There is no JWKS endpoint, no DID method, no discovery
document, and no trust-list format anywhere in the specification. A grep for
`jwks|public key|key distribution|trust list|x5c|out of band|did:` across all of
`docs/` returns exactly two hits (`specification.md:183` and `:225`).

### 9.1 The agent key IS conveyed in-band — via `cnf`

> These MUST include the agent's public key as a `cnf` claim. This is
> required as it is not yet bound to a particular transaction, and so it needs to
> be constrained for use by the Agent. It is RECOMMENDED to set the `exp` claim
> for these Mandates to the smallest value that will allow the Shopping Agent to
> complete the assigned task.
> — `docs/ap2/specification.md:225-229`

> - *cnf*: **OPTIONAL**. Contains the confirmation method identifying the
>   Proof-of-Possession key as defined in [RFC7800](#references). This claim is
>   **REQUIRED** if the Mandate is still open.
> — `docs/ap2/agent_authorization.md:441-442`

**Format: JWK, constrained to EC P-256.** `code/sdk/schemas/ap2/types/jwk.json`
(`"An EC P-256 public key in JWK format (RFC 7517)"`, `additionalProperties: false`):

| Prop | Constraint |
| --- | --- |
| `kty` | **required**, `const: "EC"` |
| `crv` | `const: "P-256"` |
| `x`, `y` | `pattern: ^[A-Za-z0-9_-]{43}$` (base64url, 43 chars = 32 bytes unpadded) |
| `use` | enum `["sig","enc"]` |
| `alg` | `const: "ES256"` |
| `kid`, `key_ops`, `x5u`, `x5c`, `x5t`, `x5t#S256` | standard RFC 7517 params |

So the *agent* key needs no distribution: it travels inside the open Mandate, and
the open Mandate's own signature is what you have to trust.

### 9.2 The user / Trusted Surface key — two models, neither with a wire protocol

> In the Direct case, the signature on the closed Mandates is validated as coming
> from a User directly, using a User Credential or a trust list of Agent
> Providers.
> — `docs/ap2/specification.md:182-184`

**Model A — User Credential (three-party).** Issuer → Holder (Trusted Surface) →
Agent. The Verifier trusts the *Issuer*.

> In this model, the Issuer of the User Credential is being trusted by the Verifier
> to ensure that the Trusted Surface constructs Mandates only after obtaining
> appropriate user consent and authorization.
> — `docs/ap2/agent_authorization.md:77-79`

> In advance of this flow, the Issuer issues the User Credential to the Holder.
> **The mechanism for issuance is outside the scope of this document**; one standard
> approach can be seen in the [OpenID4VCI](#references) specification.
> — `docs/ap2/agent_authorization.md:86-89` (emphasis added)

Presentation uses **OpenID4VP** with `transaction_data`. The mandate delegation
object MUST contain (verbatim, `agent_authorization.md:104-111`):

>   - **type**: **REQUIRED**. MUST be the string value "*delegate*".
>   - **format**: **REQUIRED**. The required VDC format of the returned Mandate.
>   - **delegate_payload**: **REQUIRED**. An array containing the Mandate Content
>     payloads as JSON Objects.
>   - **delegate_disclosures**: **OPTIONAL**. An array that contains any Selective
>     Disclosures in the `delegate_payload`.

> When constructing the Authorization Response, the `delegate_payload` MUST be
> included as part of the Key Binding.
> — `agent_authorization.md:113-114`

> It is RECOMMENDED to use the Digital Credentials API for delegation with
> OpenID4VP where available…
> — `agent_authorization.md:119-121`

The worked example uses DCQL with `format: "dc+sd-jwt"`, `vct_values:
["com.emvco.dpc"]`, `client_id_scheme: "x509_san_dns"`, and
`sd-jwt_alg_values / kb-jwt_alg_values: ["ES256"]`. The user's key appears as `cnf.jwk`
(EC P-256) inside the issuer-signed DPC credential.

**Model B — Trusted Agent Provider.** The Verifier trusts the Agent Provider
directly; no pre-issued credential.

> This allows for a simpler trust model, but requires Verifiers to establish trust
> with every Agent Provider.
> — `docs/ap2/agent_authorization.md:57-59`

> The Agent Provider MUST ensure that the Agent is not able to access the
> Agent Provider signing key, or use it without the Trusted Surface.
> — `docs/ap2/agent_authorization.md:310-312`

Its example root SD-JWT header carries `"kid": "agent-provider-key-1"` and the
payload carries `"iss": "https://agent-provider.example.com"` — but **how a Verifier
resolves `iss` + `kid` to a public key is never specified.**

### 9.3 What the SDK does **[NOT IN SPEC — implementation choice]**

`code/sdk/python/ap2/sdk/sdjwt/chain.py:41-81`, `X5cOrKidPublicKeyProvider`:
resolve the **root** token's key from the `x5c` header (DER certs, validated
against caller-supplied `trusted_roots`), else fall back to a caller-supplied
`kid_lookup: Callable[[str], JWK]`. Every subsequent hop is verified with the
*preceding* hop's `cnf.jwk`, so only the root key needs external trust:

> Verifier trusts only the root issuer key. Every hop is validated by the
> preceding hop's `cnf.jwk`…
> — `code/sdk/python/ap2/sdk/README.md:184-185`

**Practical guidance for a minimal implementation:** you must choose and document
your own root-key distribution. `x5c` + a pinned root CA, or `kid` + a
statically-configured key map, are both consistent with the spec because the spec
says nothing. Do not claim conformance on this point — there is nothing to conform to.

---

## 10. SD-JWT / chain mechanics you need to get right

### 10.1 Mandate Content claims (generic)

Verbatim, `docs/ap2/agent_authorization.md:433-447`:

> The Mandate Content for SD-JWTs contains the following claims:
>
>   - *vct*: **REQUIRED**. A String uniquely identifying the Mandate Type, in
>     addition to the credential type.
>   - *constraints*: **OPTIONAL**. An array of extensible Objects
>     providing Constraints on what is allowed to be present in the closed Mandate.
>       - *type*: **REQUIRED**. A unique String identifying this constraint.
>       - Other properties are present based on the constraint type.
>   - *cnf*: **OPTIONAL**. […] **REQUIRED** if the Mandate is still open.
>
> Other properties MAY be included in the Mandate based on the Mandate Type. A
> Mandate that is still open is NOT REQUIRED to have all of the required fields of a
> particular Mandate Type, but the eventually closed Mandate MUST include them.
> Additionally, any claim in SD-JWT-VC MAY also be used.

Also:

> Because Open Mandates need to be bound to a particular transaction before use,
> they MUST support cryptographic Key Binding.
> — `docs/ap2/agent_authorization.md:415-416`

Extension naming:

> It is RECOMMENDED to use a collision-resistant naming approach, for example via
> an rDNS prefix controlled by the specifying entity, or an appropriate URN.
> — `docs/ap2/agent_authorization.md:451-453`

### 10.2 Wire format and `typ` values

Chain hops are joined by `~~` (`sdk/README.md:132-137`):

```
<root_SD-JWT>~<disc…>~~<KB-SD-JWT+KB_1>~<disc…>~~…~~<closed_KB-SD-JWT>~<disc…>~
```

| Hop | `typ` | `cnf` | Binding |
| --- | --- | --- | --- |
| Root SD-JWT (issuer-signed) | example uses `example+sd-jwt`; DPC example uses none | **required** (endorses next hop) | — |
| Intermediate | `kb+sd-jwt+kb` | **MUST be present** | `sd_hash` (or `issuer_jwt_hash`) |
| Closed / terminal leaf | `kb+sd-jwt` | **MUST NOT be present** | `sd_hash` (or `issuer_jwt_hash`) |

SDK also accepts legacy spellings `kb-sd-jwt` / `kb-sd-jwt+kb`
(`sdjwt/kb_sd_jwt.py:30-31`).

The Mandate Content is carried in a **`delegate_payload` array** in the JWT payload
(all worked examples; `common.py:237-245`).

### 10.3 Claims on every KB hop

From `sdjwt/kb_sd_jwt.py:62-78` and `common.py:292-311`, each hop's payload carries
`iat`, `aud`, `nonce`, and exactly one of `sd_hash` / `issuer_jwt_hash`.
Verification enforces: `iat` present (always); `aud`/`nonce` matched against
expected values **on the terminal hop** when the verifier supplies them; terminal
hops MUST NOT carry `cnf`; intermediate hops MUST carry `cnf`.

Creation rejects empty `aud` or `nonce`:
`if not aud or not nonce: raise ValueError('aud and nonce are required for KB-SD-JWT hops')`.

Note: these `aud`/`nonce` requirements come from the SD-JWT KB layer and the
Delegate SD-JWT draft, **not from any MUST in the AP2 documents** — see §12.7.

### 10.4 Verification and Processing Rules (the core algorithm)

Verbatim, `docs/ap2/agent_authorization.md:455-465`:

> #### Verification and Processing Rules
>
> The verification and processing rules for a chain of SD-JWT mandates are as
> follows:
>
>   1. Verify and process the SD-JWT chain according to [Delegate SD-JWT](#references).
>   2. Extract claims from open Mandate Content and verify the closed Mandate
>      Content has these values unchanged.
>   3. Extract each Constraint from each open Mandate Content and evaluate them
>      against the closed Mandate Content based on the Constraint Type.
>      - Any unknown Constraints MUST be treated as failing evaluation.

Step 2 is easy to miss: any claim the open Mandate *does* fix (e.g. it optionally
pins `payee` or `payment_instrument`) MUST appear unchanged in the closed Mandate,
independently of constraint evaluation.

### 10.5 Per-role verification rules

**Merchant** (`docs/ap2/specification.md:302-317`), verbatim:

> The Merchant MUST receive an appropriate Checkout Mandate from a Shopping Agent
> before completing the Checkout.
>
> They MUST verify the Checkout Mandate as follows:
>
> -   Process and verify the Checkout Mandate according to
>     [Verification and Processing Rules](agent_authorization.md#verification-and-processing-rules).
> -   Verify that the hash of the Checkout JWT sent for approval matches the value
>     included for the `checkout_hash` claim.
> -   If open Checkout Mandates are included, verify that the closed Checkout
>     conforms to all of the Constraints by evaluating each Constraint.

**Credential Provider and Network** (`specification.md:319-333`), verbatim:

> They MUST verify the Payment Mandate as follows:
>
> -   Process and verify the Payment Mandate according to
>     [Verification and Processing Rules](…).
> -   If open Payment Mandates are included, verify that the closed Payment
>     Mandate matches all the Constraints.

**Merchant Payment Processor** (`specification.md:335-342`), verbatim:

> The Merchant Payment Processor MUST receive an appropriate Payment Credential
> from the Merchant before processing the transaction.
>
> Merchant Payment Processor MUST verify the Payment Credential is appropriately
> scoped to the Checkout. One way this can be done is by providing the Closed
> Payment Mandate inside the Payment Credential.

**Dispute** (`specification.md:344-364`), verbatim:

> -   The Checkout Mandate MUST be verified according to the Merchant Verification
>     rules.
> -   The hash of the `checkout_jwt` MUST be independently computed from the
>     included `checkout_jwt`.
> -   The Checkout Receipt `reference` MUST match the hash of the closed Checkout
>     Mandate. This is calculated in the same manner as the `sd_hash` would be.
> -   The Payment Mandate MUST be verified according to the
>     [Merchant Payment Processor](#merchant-payment-processor) section using the
>     `checkout_hash` from the Checkout Mandate.
> -   The Payment Receipt reference MUST match the hash of the closed Payment
>     Mandate. This is calculated in the same manner as the `sd_hash` would be.

---

## 11. Complete MUST / MUST NOT list affecting a minimal conformant implementation

Grouped by who has to do it. Every line is a direct obligation from the spec.

**Everyone**
1. MUST match the exact `vct` string including the `.1` version suffix (`specification.md:140-141`).
2. Validation/processing MUST happen in deterministic code regardless of whether the role is agentic (`specification.md:96-98`).
3. Unknown constraints MUST be treated as failing evaluation (`agent_authorization.md:465`).
4. Digests in SD-JWTs MUST include a salt with sufficient entropy (RFC 9901 §9.1) (`security…md`, §Rainbow Table Attacks).

**Merchant**
5. MUST provide a merchant-signed JWT containing the Checkout to the SA (`specification.md:126-127`).
6. The Checkout JWT MUST be signed with a non-deterministic scheme (ECDSA), NOT a deterministic one (Ed25519); if a deterministic scheme is used, a sufficient-entropy salt MUST be in the Checkout (`specification.md:154-157`; `security…md`).
7. MUST receive an appropriate Checkout Mandate before completing the Checkout (`specification.md:304-305`).
8. MUST verify `checkout_hash` matches the hash of the *latest* `checkout_jwt` (`specification.md:311-312`; `security…md` §Manipulated Checkout).
9. MUST return a Checkout Receipt on accept **or** reject; on failure it MUST be a Checkout Receipt JWT carrying the error (`specification.md:130-131, 316-317`).

**Shopping Agent**
10. Open Mandates MUST include the agent's public key as a `cnf` claim (`specification.md:225-226`).
11. MUST NOT present subsequent open Payment/Checkout Mandates without a rejection receipt for the previous one (`specification.md:237-240`).
12. MUST present only the disclosures needed to evaluate the closed Mandates (`specification.md:242-243`); when presenting, MUST choose disclosures to maximise privacy while still authorising (`agent_authorization.md:486-488`).
13. The non-deterministic portion MUST avoid signing multiple overlapping closed Mandates for the same open Mandate without rejection receipts; those receipts MUST be integrity-protected from the SA's LLM (`security…md` §Double Spend).
14. MUST provide both the user-signed open Mandate and the agent-signed closed Mandate to Verifying Parties (`specification.md:233-235`).

**Credential Provider / Network**
15. MUST receive an appropriate Payment Mandate before returning a payment credential (`specification.md:321-323`).
16. MUST verify the User's signature on the Payment Mandate (`security…md` §Manipulated Payment).
17. MUST return a Payment Receipt JWT with the error on any verification failure (`specification.md:332-333`).
18. The Payment Credential/Token MUST ONLY be released to the Merchant upon receipt and verification of a final Payment Mandate (`security…md` §Payment Credential Theft).

**Merchant Payment Processor**
19. MUST receive an appropriate Payment Credential from the Merchant before processing (`specification.md:337-338`).
20. MUST verify the Payment Credential is appropriately scoped to the Checkout (`specification.md:340-341`).
21. A signed Payment Receipt MUST be returned to SA, CP and possibly Networks on accept or reject (`specification.md:159-161`).

**Trusted Surface**
22. MUST be non-agentic (`specification.md:78-80`).
23. (Trusted Agent Provider model) The Agent Provider MUST ensure the Agent cannot access the signing key or use it without the Trusted Surface (`agent_authorization.md:310-312`).

**Structural / crypto**
24. Closed Mandates MUST contain `sd_hash` to bind to the presented open Mandate (`security…md` §Manipulated Checkout).
25. Open Mandates MUST support cryptographic Key Binding (`agent_authorization.md:415-416`).
26. The closed Mandate MUST include all required fields of its Mandate Type (open ones need not) (`agent_authorization.md:444-446`).
27. Open Checkout Mandate `constraints` MUST contain a `checkout.line_items`; open Payment Mandate `constraints` MUST contain a `payment.reference` (JSON Schema `contains`).
28. Selective Disclosure MUST be used to avoid leaking inapplicable open-Mandate constraints (`security…md` §Privacy).
29. The Payment Mandate MUST contain a reference to its Checkout — `transaction_id` (closed) / `payment.reference` constraint (open) (`security…md` §Manipulated Checkout).

**RECOMMENDED / SHOULD / MAY worth honouring**
- RECOMMENDED: set `exp` on open Mandates to the smallest value that lets the SA finish the task (`specification.md:227-229`).
- RECOMMENDED: use the Digital Credentials API for OpenID4VP delegation where available (`agent_authorization.md:119-121`).
- SHOULD: the SA provides a mechanism to manage active Mandates (`implementation_considerations.md` §Mandate Management).
- MAY: the Trusted Surface inserts decoy digests per RFC 9901 §4.2.5 (`security…md` §Privacy).
- MAY: CP/Networks/MPPs reject multiple overlapping Mandates or invalidate previously issued tokens (`security…md` §Double Spend).

---

## 12. Where the spec is silent, ambiguous, or self-contradictory

Document these as **our choices**, not as conformance.

### 12.1 ⚠️ BIGGEST ONE — Receipt `reference` is computed two incompatible ways

- **The spec** says (twice, §4.5): `reference` is "calculated in the same manner as
  `sd_hash`", i.e. over `issuer_jwt + '~' + disclosures.join('~') + '~'`.
- **The reference SDK** says (`sdk/README.md:90, 103-109, 293-297`):
  ```python
  reference = compute_sha256_b64url(MandateClient().get_closed_mandate_jwt(chain))
  ```
  where `get_closed_mandate_jwt` = `presentation_token.rsplit('~~', 1)[-1].split('~', 1)[0]`
  (`sdk/mandate.py:416-417`) — **the bare leaf issuer JWT: no disclosures, no
  trailing `~`.** That is the `issuer_jwt_hash` manner, not the `sd_hash` manner.

These produce **different values** for the same mandate. The SDK's own rationale
(`mandate.py:411-414`) is that it wants a reference "stable across delegation hops
and disclosure choices" — which the spec's `sd_hash`-style definition is *not*,
since it changes whenever the SA redacts a different disclosure set.

**Our choice must be explicit.** The spec text is normative but the SDK behaviour is
what any Google-sample-based counterparty will actually emit. Recommend:
implement the SDK behaviour (leaf-JWT hash) for interop, store both, and document
the deviation loudly. Do not silently claim spec conformance on `reference`.

### 12.2 Receipt field name and enum casing contradict between layers

`agent_authorization.md:507` defines **`result`** with enum **`["success","error"]`**;
every AP2 schema, generated model and sample uses **`status`** with enum
**`["Success","Error"]`**. The schemas also require `payment_id` on Payment
Receipts, which the generic definition never mentions. → Implement `status` /
`Success` / `Error`; treat `result` as an editorial leftover.

### 12.3 `checkout_jwt` encoding is described inconsistently

The schema says *"base64url-encoded serialized merchant-signed JWT of the Checkout
payload"*, but a compact JWS is *already* base64url-segmented, and the samples store
the compact string as-is and hash that string directly. A literal reading ("take the
compact JWT, base64url it again") would produce a different `checkout_hash`. →
**Use the compact JWS string verbatim**, both as the claim value and as the hash
input. That is what the samples and all worked examples do.

### 12.4 Hash input character encoding is unspecified

SDK SD-JWT hashing uses `.encode('ascii')` (`common.py:166`); the sample's
`checkout_hash` helper uses `.encode()` = UTF-8 (`merchant_agent/tools.py:89`).
Identical for well-formed base64url JWTs (pure ASCII), but the spec never states
it. → Specify ASCII/UTF-8 explicitly in our implementation; reject non-ASCII in
token strings.

### 12.5 base64url padding is unspecified

The SDK strips `=` padding (`utils.py:23`, `.rstrip(b'=')`). The spec only says
"base64url-encoded". → Emit unpadded; accept both on input.

### 12.6 Amount typing is inconsistent

`amount_range.max`/`min` are `integer` minor units in the schema, but the doc
example at `payment_mandate.md:183-190` shows `"max": 100.50, "min": 10.00`
(decimals). `budget.max` is typed `number` while `amount_range.max` is `integer`.
`types/amount.json` `amount` is `integer` minor units. → **Use integer minor units
everywhere**, including `budget.max`; treat the decimal doc examples as errata.
Also note `amount_range` compares against `payment_amount.amount` (an object
member), which the prose glosses as "the `payment_amount` property".

### 12.7 `aud` / `nonce` have no AP2-level normative requirements

Nothing in `docs/` states that a closed Mandate MUST carry `aud`/`nonce`, who
generates the nonce, its required entropy, an acceptable replay window, or clock-skew
tolerance. The SDK requires both to be non-empty at creation and matches them only
when the verifier passes expected values. Worked examples use `aud: "merchant"` /
`"credential-provider"` and a 32-hex-char nonce. → Define our own: verifier-generated
nonce, ≥128 bits, single-use with a bounded TTL, `aud` = a stable verifier identifier.

### 12.8 No `exp` / `iat` enforcement rules

`iat`/`exp` are optional on every mandate schema, and no document says a verifier
MUST reject an expired mandate or bound `iat` skew. Only a RECOMMENDED "set `exp`
small" for open mandates. → We MUST enforce `exp` and a skew window ourselves and
say so.

### 12.9 No key distribution / discovery mechanism at all

See §9. No JWKS URL, no DID, no `iss`→key resolution, no trust-list format, no
revocation. Even credential *issuance* is explicitly out of scope
(`agent_authorization.md:87-88`). → Our root-key trust model is entirely our own
design decision.

### 12.10 Constraint type name typo in the security doc

`security_and_privacy_considerations.md` refers to "the `mandate.payment.reference`
constraint"; the actual type string is **`payment.reference`** (schema `const`,
worked examples). → Use `payment.reference`.

### 12.11 `allowed_pisps` is not selectively disclosable

`allowed_payees` and `allowed_payment_instruments` carry
`x-selectively-disclosable-array: true`; `allowed_pisps` does not. Given the privacy
MUST in §11.28, this looks like an oversight rather than intent. → Note it; do not
rely on redacting PISP lists.

### 12.12 Two different `item` shapes

`open_checkout_mandate.json#/$defs/item` requires `{id, title}`;
`ap2/types/item.json` requires `{id, title, price}`. Constraint matching uses the
former (ID-based). → Use the `$defs` shape inside constraints; the `types` shape
only inside the UCP Checkout.

### 12.13 Human-Present chain shape is under-specified

In Direct mode there is no open Mandate, yet the closed Mandate is still a
KB-SD-JWT that structurally needs a preceding token with a `cnf` to sign under. The
OpenID4VP example resolves this by making the *user's DPC credential* the root
(its `cnf.jwk` is the user key, and `delegate_payload` rides in the Key Binding),
but the Trusted-Agent-Provider variant of Direct mode is never drawn end to end.
→ Document our chosen Direct-mode chain shape explicitly.

### 12.14 Double-spend / budget tracking needs unspecified verifier state

`payment.budget` and `payment.agent_recurrence` both require the verifier to persist
cumulative spend and presentation history across transactions
(`payment_mandate.md:58-62, 204-208`), but no storage model, scope key, or
cross-verifier coordination is defined. With multiple CPs, the accounting is
simply unsound as specified. → Scope our accounting per (open mandate `sd_hash`,
verifier) and document the limitation.

### 12.15 Receipt JWT header unspecified

No `alg`/`typ`/`kid` requirements for the receipt JWT. Samples use ES256 with a
plain JWT header. → Pin ES256.

### 12.16 ⚠️ The pretty-printed JSON examples contradict their own encoded tokens

The `docs/ap2/checkout_mandate.md` examples give both a human-readable JSON block
and, below it, the actual encoded token. **They disagree.** The encoded tokens are
internally consistent and cryptographically correct (§4.6); the JSON blocks contain
transcription errors. Confirmed divergences:

| Location | JSON block says | Encoded token actually contains |
| --- | --- | --- |
| `checkout_mandate.md:272` | `checkout_jwt` disclosure `digest` = `FzLoxbbtgQGYZxoSM2NJYJtkFTSsdfUBoVEQ12k7JN8` | `3A9UyZJofw2eMP-Lx2tYaNpCcuB8elnhwwLhZLwqQFM` — the JSON block has mistakenly reused the `sd_hash` value here |
| `checkout_mandate.md:314` | `_sd: ["3A9UyZJofw2eMP-Lx2tYaNpCcuB8elnhwwLhZLwqFFM"]` (…`FFM`) | …`LwqQFM` (…`QFM`) |
| `checkout_mandate.md:228` | `cnf.jwk.x` = `QpSyxPQHy38xckypDr54…` (`ckyp`) | `QpSyxPQHy38xckyvDr54…` (`ckyv`) |
| `checkout_mandate.md:229` | `cnf.jwk.y` = `37HLd7JJinxjJIn8J7Hijssoec**lb**fhdW-gUL7feI9lw` | `…jssoec**Bl**fhdW…` |

Consequence: **the same digest string `FzLox…` appears in the docs playing three
different roles** — as the closed Mandate's `sd_hash` (correct), as the open Payment
Mandate's `conditional_transaction_id` (correct — it is genuinely the same value,
see §4.6), *and* as the `checkout_jwt` disclosure digest (wrong). An implementer who
builds test fixtures from the JSON blocks will produce mandates that fail
verification.

→ **Build test vectors only from the encoded tokens.** They decode cleanly and every
hash reproduces. The observed open-mandate lifetime in the vectors is `exp - iat =
3600` seconds.

### 12.17 Other out-of-scope declarations (for completeness)

- Commerce Protocol details (catalog APIs, checkout updates, inter-role APIs) — `specification.md:20-24`.
- Agent-to-agent Mandate delegation — `specification.md:256-260`.
- Dispute retention/retrieval mechanics — `specification.md:262-268`; automated Checkout Mandate retrieval "would be done by using the Payment Mandate `transaction_id` as the key" but is explicitly deferred (`specification.md:286-290`).
- How the SA determines user intent / assembles Mandate Content — `specification.md:109-111`.
- Mandate selection mechanism — `agent_authorization.md:490-491`, `flows.md:141-142`.
- Agent identification / whether to only work with trusted agents — "left to the Commerce Protocol layer" (`implementation_considerations.md` §Agent Identification).

---

## 13. Minimal conformant build order (derived)

1. **Merchant:** build UCP Checkout → sign compact JWS with ES256 → `checkout_jwt`; compute `checkout_hash = b64url_nopad(SHA-256(checkout_jwt))`.
2. **SA:** assemble Checkout Mandate Content (`vct=mandate.checkout.1`, `checkout_jwt`, `checkout_hash`) and Payment Mandate Content (`vct=mandate.payment.1`, `transaction_id=checkout_hash`, `payee`, `payment_amount`, `payment_instrument`).
3. **TS:** render both, obtain consent, sign. Direct → sign the closed Mandates. Autonomous → sign open Mandates instead, each with `cnf.jwk` = agent EC P-256 key, `exp` short, open Checkout with ≥1 `checkout.line_items`, open Payment with a `payment.reference` whose `conditional_transaction_id` = `sd_hash` of the open Checkout Mandate.
4. **SA (autonomous):** key-bind — emit `typ=kb+sd-jwt` leaf with `iat`, `aud`, `nonce`, `sd_hash` over the preceding hop, no `cnf`, `delegate_payload=[closed Mandate Content]`, joined to the open token by `~~`. Disclose only what is needed.
5. **CP:** verify chain → constraints → release token scoped to the closed Payment Mandate. On failure, signed Payment Receipt with `status: "Error"` + `error` from the four-code list.
6. **Merchant:** verify chain → `checkout_hash` vs *latest* `checkout_jwt` → constraints → complete → signed Checkout Receipt (`Success` ⇒ `order_id`).
7. **MPP:** verify Payment Mandate inside the credential + checkout binding → signed Payment Receipt (`Success` ⇒ `payment_id`, `psp_confirmation_id`, `network_confirmation_id`) to SA, CP, Network.
8. **Store** every SD-JWT with its disclosures in compact serialization, for dispute (`implementation_considerations.md` §Hashes).

---

## 14. Local paths for re-checking

```
Repo:      /private/tmp/claude-501/-Users-mika-Repos-Automate-me/34bd1a9e-4dbb-439e-8083-c352b78d233b/scratchpad/AP2
Specs:     docs/ap2/{specification,checkout_mandate,payment_mandate,agent_authorization,flows,
                     implementation_considerations,security_and_privacy_considerations}.md
Schemas:   code/sdk/schemas/ap2/{checkout_mandate,open_checkout_mandate,payment_mandate,
                                 open_payment_mandate,checkout_receipt,payment_receipt}.json
           code/sdk/schemas/ap2/types/{amount,item,jwk,merchant,payment_instrument,pisp,receipt_status}.json
           code/sdk/schemas/ucp/types/{checkout,line_item,total,buyer,link,message*}.json
Hashing:   code/sdk/python/ap2/sdk/sdjwt/common.py  (compute_sd_hash, compute_issuer_jwt_hash, verify_binding)
           code/sdk/python/ap2/sdk/utils.py         (b64url_encode, compute_sha256_b64url)
KB hops:   code/sdk/python/ap2/sdk/sdjwt/kb_sd_jwt.py
Chain:     code/sdk/python/ap2/sdk/sdjwt/chain.py   (verify_chain, X5cOrKidPublicKeyProvider)
Receipts:  code/sdk/python/ap2/sdk/receipt_wrapper.py, jwt_helper.py
Reference: code/samples/python/src/roles/merchant_agent/tools.py  (checkout JWT + checkout_hash)
SDK notes: code/sdk/python/ap2/sdk/README.md
```
