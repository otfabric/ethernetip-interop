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
| `libplctag` | [libplctag](https://github.com/libplctag/libplctag) | v2.7.2 | commit `7712ed7f1ee05ba0ebfca641b3f5c43da52a66ed`, GitHub archive SHA-256 `ed1f7869fa266df1bc266b9d135cd09d53dc392e557edb9986b2e172c84eb752` | independent, C |
| `gologix` | [gologix](https://github.com/danomagnum/gologix) | v0.44.0-beta | commit `647a0b87b756d42e37ac0f65c5762dc89036152c`, Go module sum `h1:QrkDcR4ZFmtZ2lhWpdiE+8025nTwO4BulJkPZV+wsp8=` | independent, Go |
| `pylogix` | [pylogix](https://github.com/dmroeder/pylogix) | 1.1.6 | PyPI wheel SHA-256 `f181ff0990468c2e7fe0985be190c0bfe9c0d54dc4bb3f28f5a6d76b92cf980f` (commit `193b6c7b0c601619223ee53587abd821d3b344c1`) | independent, Python |
| `etherip` | [etherip](https://github.com/ornl-epics/etherip) | main, V1.0-67 | commit `ce0688ae51be5652ec200fbe389667b36a638323`, GitHub archive SHA-256 `35ad2cbb710be5bef6cae0b2441a9397534249ddb33e9ac4e1e377b2e6546c7e` | independent, Java |
| `eiprs` | [eip-rs (rseip)](https://github.com/Joylei/eip-rs) | main, 0.3.1-2 | commit `16e2236afb65acc9c82ebdcfbdcbba72e93b3887`, GitHub archive SHA-256 `92cccb84822056e23ab93a2d200f11ebf7a3c6813648eaf081d653d2961da77b`; crates in `Cargo.lock` | independent, Rust |
| `nodeeip` | [node-ethernet-ip](https://github.com/cmseaton42/node-ethernet-ip) | 2.0.0 | npm integrity `sha512-jGxbu8ifv2qgPzaNKbyFmLaOaefrsg3aVsBWEj3BTWyGSfaVRx/MnkNJuiR/+41GzWgt/LZpYnFGJnAnjO7DCA==` (commit `4743b077abd3cc8826d511acde8f890372826e16`) | independent, TypeScript |
| `eeip` | [EEIP.NET](https://github.com/rossmann-engineering/EEIP.NET) | master, 1.6.0 | commit `58b91a0c8e30227b1b97a637635d147a2a9655dc`, GitHub archive SHA-256 `8ad4f3e498b6d02b1c65b8fae498100a9f9f90d6f691423e5c80559c4eceff5b` | independent, C# |

OpENer, EIPScanner, etherip, eip-rs and EEIP.NET are pinned to a commit of
their default branch (no recent release, or none at all). Base images, by
digest: Debian 13 (trixie) slim and Python 3.14.8 slim-trixie (recorded
2026-10-19); golang 1.27.3, Rust 1.98.1, Node.js 24.22.1, Maven 3.9.11 with
Temurin 21, the Temurin 21 JRE and .NET 10 (recorded 2026-10-26).

The pins live in the Dockerfiles, the Python adapters' `requirements.txt` and
(OpENer) `adapters/opener/capabilities.json`, and are mirrored in
[versions.yaml](../versions.yaml); `scripts/check-versions.sh` keeps them in
step.

## Cross-stack matrix

`make selftest` (`scripts/selftest.sh`) starts each server image (opener,
cpppo, libplctag, gologix) with its first fixture on a private Docker
network and runs every client image against it, case by case. A case runs
when the client has the operation and both sides declare the features it
needs; otherwise it is n/a, with the reason in `summary.tsv`. Expected values
come from the server's fixture. A server that a case brings down is
restarted, and the crash is recorded against that case. Every result
document is kept under `reports/selftest/` and validated against
`schemas/client-result.schema.json`.

Some declared features do not work between two particular references. These
**known incompatibilities** are listed, each with its reason, in
[scripts/selftest-known.tsv](../scripts/selftest-known.tsv) (and described in
[CAPABILITIES.md](CAPABILITIES.md)): such a case is recorded `known` and does
not fail the self-test; one that starts passing is reported, so that its
entry is removed.

Run of 2026-10-26 on linux/arm64, images built from this repository
(`make images`, tag `dev`): **171 cases passed, 0 failed, 22 known
incompatibilities, 568 n/a; 215 documents valid against the schemas.**
`make smoke`: every adapter, none failed.

#### Server opener

| Case | eipscanner | cpppo | pycomm3 | libplctag | gologix | pylogix | etherip | eiprs | nodeeip | eeip |
|------|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| ListIdentity (TCP) | n/a | pass | pass | n/a | pass | n/a | known | pass | pass | n/a |
| ListIdentity (UDP) | pass | pass | pass | n/a | n/a | pass | known | pass | pass | pass |
| ListServices | n/a | pass | n/a | n/a | pass | n/a | known | pass | n/a | n/a |
| Get_Attribute_Single | pass | pass | pass | known | pass | pass | pass | pass | pass | pass |
| Get_Attributes_All (Identity) | pass | pass | pass | known | pass | pass | pass | pass | pass | pass |
| Set_Attribute_Single (assembly data) | pass | pass | pass | known | pass | pass | n/a | pass | pass | pass |
| Error status (unknown attribute) | pass | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| Multiple Service Packet | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 3 connected explicit messaging | n/a | n/a | pass | pass | pass | n/a | n/a | pass | known | n/a |
| identity operation | pass | n/a | n/a | pass | n/a | n/a | n/a | n/a | pass | pass |
| Class 1 I/O, exclusive owner | pass | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | pass |
| Class 1 I/O, Large_Forward_Open | pass | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | pass |
| Class 1 I/O, input only | pass | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | pass |
| Forward_Open refusal (connection size) | pass | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | pass |
| Class 1 I/O, configuration data | pass | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Read Tag (atomic types) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Read Tag Fragmented | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Write Tag | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix tag list | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

#### Server cpppo

| Case | eipscanner | cpppo | pycomm3 | libplctag | gologix | pylogix | etherip | eiprs | nodeeip | eeip |
|------|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| ListIdentity (TCP) | n/a | pass | pass | n/a | pass | n/a | pass | pass | pass | n/a |
| ListIdentity (UDP) | pass | pass | pass | n/a | n/a | pass | pass | pass | pass | pass |
| ListServices | n/a | pass | n/a | n/a | known | n/a | known | known | n/a | n/a |
| Get_Attribute_Single | pass | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| Get_Attributes_All (Identity) | pass | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| Set_Attribute_Single (assembly data) | pass | pass | pass | pass | pass | pass | n/a | pass | pass | pass |
| Error status (unknown attribute) | pass | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| Multiple Service Packet | pass | pass | n/a | n/a | pass | n/a | n/a | pass | pass | n/a |
| Class 3 connected explicit messaging | n/a | n/a | pass | known | pass | n/a | n/a | pass | pass | n/a |
| identity operation | pass | n/a | pass | pass | n/a | pass | pass | n/a | pass | pass |
| Class 1 I/O, exclusive owner | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 1 I/O, Large_Forward_Open | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 1 I/O, input only | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Forward_Open refusal (connection size) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 1 I/O, configuration data | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Read Tag (atomic types) | n/a | pass | n/a | n/a | n/a | n/a | known | n/a | n/a | n/a |
| Logix Read Tag Fragmented | n/a | pass | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Write Tag | n/a | pass | n/a | n/a | n/a | n/a | pass | n/a | n/a | n/a |
| Logix tag list | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

#### Server libplctag

| Case | eipscanner | cpppo | pycomm3 | libplctag | gologix | pylogix | etherip | eiprs | nodeeip | eeip |
|------|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| ListIdentity (TCP) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| ListIdentity (UDP) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| ListServices | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Get_Attribute_Single | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Get_Attributes_All (Identity) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Set_Attribute_Single (assembly data) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Error status (unknown attribute) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Multiple Service Packet | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 3 connected explicit messaging | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| identity operation | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 1 I/O, exclusive owner | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 1 I/O, Large_Forward_Open | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 1 I/O, input only | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Forward_Open refusal (connection size) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 1 I/O, configuration data | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Read Tag (atomic types) | n/a | pass | n/a | n/a | n/a | n/a | known | n/a | n/a | n/a |
| Logix Read Tag Fragmented | n/a | pass | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Write Tag | n/a | pass | n/a | n/a | n/a | n/a | known | n/a | n/a | n/a |
| Logix tag list | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

#### Server gologix

| Case | eipscanner | cpppo | pycomm3 | libplctag | gologix | pylogix | etherip | eiprs | nodeeip | eeip |
|------|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| ListIdentity (TCP) | n/a | pass | pass | n/a | pass | n/a | pass | pass | pass | n/a |
| ListIdentity (UDP) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| ListServices | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Get_Attribute_Single | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Get_Attributes_All (Identity) | pass | pass | pass | pass | pass | pass | pass | pass | pass | pass |
| Set_Attribute_Single (assembly data) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Error status (unknown attribute) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Multiple Service Packet | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Class 3 connected explicit messaging | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| identity operation | pass | n/a | pass | pass | n/a | pass | pass | n/a | pass | pass |
| Class 1 I/O, exclusive owner | known | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | known |
| Class 1 I/O, Large_Forward_Open | known | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | known |
| Class 1 I/O, input only | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Forward_Open refusal (connection size) | known | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | known |
| Class 1 I/O, configuration data | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Read Tag (atomic types) | n/a | pass | n/a | n/a | n/a | n/a | known | n/a | n/a | n/a |
| Logix Read Tag Fragmented | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |
| Logix Write Tag | n/a | known | n/a | n/a | n/a | n/a | pass | n/a | n/a | n/a |
| Logix tag list | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

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
Get_Attributes_All and the `identity` operation (between most pairs), error
statuses, Multiple Service Packet (EIPScanner, eip-rs, gologix and
node-ethernet-ip → cpppo), Class 3 (eip-rs, gologix, libplctag, pycomm3 →
OpENer; eip-rs, gologix, node-ethernet-ip, pycomm3 → cpppo), Class 1 I/O
(EIPScanner and EEIP.NET → OpENer, with Large_Forward_Open and input-only;
configuration data from EIPScanner), and Logix tag services (cpppo →
libplctag's ab_server, including Read Tag Fragmented; cpppo → gologix;
etherip → cpppo and gologix). The Logix originators that open with a tag
list (pycomm3, pylogix, libplctag, eip-rs, node-ethernet-ip, gologix) have no
reference target serving a usable one: cpppo and ab_server have none, and
gologix's reply carries only names and types.

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
