# Compatibility

Which upstream versions the adapters are built on, what the reference stacks
were verified to do against each other, and what was found on the way.

Everything here is about the reference stacks. It makes no statement about
any library that consumes the images.

## Pinned upstreams

| Adapter | Upstream | Pinned | Pin | Engine |
|---------|----------|--------|-----|--------|
| `opener` | [OpENer](https://github.com/EIPStackGroup/OpENer) | master, v2.3-588 | commit `d8820ef41ce30dda06d213438ba81c81de33ffe2`, GitHub archive SHA-256 `6c34be6bfe294351a69544722079751f96cdc24ac1a43337dd3e22d843f5b95b` | independent, C |
| `eipscanner` | [EIPScanner](https://github.com/nimbuscontrols/EIPScanner) | master, 1.3.0-33 | commit `12c89a57ec2e7449efb1348be52af10be9ef9701`, GitHub archive SHA-256 `3efbb95bff91501328b5899dd635cfee61ec11f59f3ad7609812fb65211ddc63` | independent, C++ |
| `cpppo` | [cpppo](https://github.com/pjkundert/cpppo) | 5.2.5 | PyPI wheel SHA-256 `0257ba393f1e2bdd06ea712cead740e496c802b3e5275c1995ee2df31f29ed1c` (tag commit `f7c224bbb2389bd26d0088eeb1a93db3a5c4858f`); greenery 2.1, sdist SHA-256 `759c3bf40b6fe6ef0a6b373230d07470e0e315f32bcdbaf381901b0d8a6f91d1` | independent, Python |
| `pycomm3` | [pycomm3](https://github.com/ottowayi/pycomm3) | 1.2.16 | PyPI wheel SHA-256 `88e95c70472154ee32233f76af126526db208830e1e803eb9f621c8bd1d8fcd0` (commit `36aeb19b5cb47495df9fd1caa38ce9d2f6d12ea3`) | independent, Python |

OpENer and EIPScanner have made no release in years; both are pinned to a
commit of their default branch. Base images: Debian 13 (trixie) slim and
Python 3.14.8 slim-trixie, by digest (recorded 2026-10-19).

The pins live in the Dockerfiles, the Python adapters' `requirements.txt` and
(OpENer) `adapters/opener/capabilities.json`, and are mirrored in
[versions.yaml](../versions.yaml); `scripts/check-versions.sh` keeps them in
step.

## Cross-stack matrix

`make selftest` (`scripts/selftest.sh`) starts each server image (opener,
cpppo) with its own fixture on a private Docker network and runs every
client image (eipscanner, cpppo, pycomm3) against it, case by case. A case
runs when the client has the operation and both sides declare the features
it needs; otherwise it is n/a, with the reason in `summary.tsv`. Expected
values come from the server's fixture. Every result document is kept under
`reports/selftest/` and validated against `schemas/client-result.schema.json`.

Run of 2026-10-26 on linux/arm64 (Docker 29.9.2), images built from this
repository (`make images`, tag `dev`): **50 cases passed, 0 failed, 58 n/a;
57 documents valid against the schema.** `make smoke`: opener 21, eipscanner
23, cpppo 30, pycomm3 20 checks, none failed. Not yet run on linux/amd64
(CI's amd64 job will be the first).

| Case | eipscanner → opener | cpppo → opener | pycomm3 → opener | eipscanner → cpppo | cpppo → cpppo | pycomm3 → cpppo |
|------|:-:|:-:|:-:|:-:|:-:|:-:|
| ListIdentity (TCP) | n/a ¹ | pass | pass | n/a ¹ | pass | pass |
| ListIdentity (UDP) | pass | pass | pass | pass | pass | pass |
| ListServices | n/a ² | pass | n/a ² | n/a ² | pass | n/a ² |
| Get_Attribute_Single (Identity name) | pass | pass | pass | pass | pass | pass |
| Get_Attributes_All (Identity) | pass | pass | pass | pass | pass | pass |
| Set_Attribute_Single (assembly data, echoed or read back) | pass | pass | pass | pass | pass | pass |
| Error status (unknown attribute) | pass (0x14) | pass (0x14) | pass (0x14) | pass (0x08) | pass (0x08) | pass (0x08) |
| Multiple Service Packet | n/a ³ | n/a ³ | n/a ² | pass | pass | n/a ² |
| Class 3 connected explicit messaging | n/a ⁴ | n/a ⁴ | pass | n/a ⁴ | n/a ⁴ | pass |
| `identity` operation | pass | n/a ² | n/a ⁵ | pass | n/a ² | pass |
| Class 1 I/O, exclusive owner (echo) | pass | n/a ² | n/a ² | n/a ⁶ | n/a ² | n/a ² |
| Class 1 I/O, Large_Forward_Open | pass | n/a ² | n/a ² | n/a ⁶ | n/a ² | n/a ² |
| Class 1 I/O, input only | pass | n/a ² | n/a ² | n/a ⁶ | n/a ² | n/a ² |
| Forward_Open refusal (connection size) | pass | n/a ² | n/a ² | n/a ⁶ | n/a ² | n/a ² |
| Logix Read Tag (atomic types) | n/a ² | n/a ⁷ | n/a ⁷ | n/a ² | pass | n/a ⁸ |
| Logix Read Tag Fragmented (300 DINT) | n/a ² | n/a ⁷ | n/a ⁷ | n/a ² | pass | n/a ⁸ |
| Logix Write Tag (and read back) | n/a ² | n/a ⁷ | n/a ⁷ | n/a ² | pass | n/a ⁸ |
| Logix tag list | n/a ² | n/a ² | n/a ⁸ | n/a ² | n/a ² | n/a ⁸ |

¹ EIPScanner sends ListIdentity over UDP only.
² the client has no such operation.
³ OpENer has no Multiple Service Packet (answers 0x08).
⁴ the client (adapter) has no Class 3.
⁵ pycomm3's `identity` is routed through Unconnected_Send, which OpENer does not have.
⁶ cpppo has no Class 1 I/O.
⁷ OpENer is not a Logix controller.
⁸ pycomm3's `LogixDriver` uploads the tag list first, and neither target serves one (0x55).

What the cases check: the identity, the name (`@1/1/7`) and the start of
Get_Attributes_All against the fixture; Set_Attribute_Single on the output
assembly, then the input assembly (OpENer's echo) or the output assembly read
back (cpppo); a non-zero status for attribute 99 of the Identity object;
both replies of a Multiple Service Packet; for Class 1 the Forward_Open
status, at least 10 packets each way (input only: received) at 10 ms RPI, no timeout,
the output echoed into the last input (a pattern that set-attribute did not
leave there), Forward_Close status 0, and a Forward_Open with a wrong size
refused; for Logix the values of every scalar tag but the BOOL, all 300
elements of `Samples`, and `Speeds` after writing three elements.

So, by feature, verified between at least two independent stacks: ListIdentity
over TCP and UDP, ListServices, Get_Attribute_Single, Set_Attribute_Single,
Get_Attributes_All, error statuses, Multiple Service Packet, Unconnected_Send
(pycomm3 → cpppo), Class 3 (pycomm3 → OpENer and cpppo) and Class 1 I/O
(EIPScanner → OpENer). Logix tag services are verified only by cpppo against
itself here: the other Logix originator, pycomm3, needs a tag list that no
reference target serves.

## Findings

- **cpppo's server ignored the fixture's identity.** `--no-config` disables
  every configuration file, the one given with `--config` included, so cpppo
  served its built-in 1756-L61 identity (serial 7079450, state 255, address
  0.0.0.0). The adapter now loads its generated configuration: the fixture's
  identity, state 3 and the container's address.
- **cpppo 5.2.5 packs the UINTs of its ListIdentity reply as signed 16-bit
  integers** (`'h'`): with vendor id 65535 it raises and answers nothing.
  The adapter refuses fixtures with a vendor id, device type or product code
  above 32767 (exit 4); hence it cannot serve the `adapter` fixture.
- **cpppo's simulator signals its whole process group when it stops**: with
  the adapter in the same group, a SIGTERM ended in SIGKILL (exit 137). The
  simulator now runs in a session of its own, and the adapter stops it outside
  the signal handler; SIGTERM gives exit 0 and the `stopped` event.
- OpENer answers 0x14 for an unknown attribute of the Identity object, cpppo
  0x08 (service not supported).
- EIPScanner reports a refused Forward_Open only in its log; the adapter
  reports it as `refused` with status -1.
