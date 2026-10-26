# Third-party notices

The images built from this repository contain third-party software. This
file is copied into every image as `/licenses/THIRD_PARTY_NOTICES.md`, next
to the licence texts. All four images are published.

## What is licensed how

| Part | Licence |
|------|---------|
| Repository tooling, fixtures, schemas, scripts, documentation | MIT ([LICENSE](LICENSE)) |
| `adapters/opener/` (runs OpENer's sample application) | MIT |
| `adapters/eipscanner/` (links EIPScanner) | MIT |
| `adapters/pycomm3/` (imports pycomm3) | MIT |
| `adapters/cpppo/` (imports cpppo) | GPL-3.0-or-later ([adapters/LICENSE](adapters/LICENSE)) |
| `adapters/libplctag/` (links libplctag), `adapters/gologix/` (links gologix), `adapters/pylogix/`, `adapters/etherip/`, `adapters/eiprs/`, `adapters/nodeeip/`, `adapters/eeip/`, `adapters/lokieip/` (links go-ethernet-ip) | MIT |
| Image `ethernetip-interop-opener` | MIT and the OpENer licence (adapted BSD, see below), and the licences of the Debian packages |
| Image `ethernetip-interop-eipscanner` | MIT, and the licences of the Debian packages |
| Image `ethernetip-interop-pycomm3` | MIT, and the Python licence |
| Image `ethernetip-interop-cpppo` | **GPL-3.0 as a whole** (it contains cpppo and the GPL adapter), and the licences of its components |
| Image `ethernetip-interop-libplctag` | MIT and libplctag (MPL-2.0 OR LGPL-2.0-or-later, used under MPL-2.0), and the licences of the Debian packages |
| Image `ethernetip-interop-gologix` | MIT and crc16 (BSD-2-Clause), and the licences of the Debian packages |
| Image `ethernetip-interop-pylogix` | MIT and pylogix (Apache-2.0), and the Python licence |
| Image `ethernetip-interop-etherip` | MIT and etherip (EPL-1.0), and the Eclipse Temurin JRE (GPL-2.0 with the Classpath Exception) |
| Image `ethernetip-interop-eiprs` | MIT, and the MIT/Apache-2.0 licences of the Rust crates, and the licences of the Debian packages |
| Image `ethernetip-interop-nodeeip` | MIT, and the licences of Node.js |
| Image `ethernetip-interop-eeip` | MIT, and the licences of the .NET runtime image |
| Image `ethernetip-interop-lokieip` | MIT and WTFPL, and the licences of the Debian packages |

Each adapter is a separate program in a separate image; no image contains two
implementations. Nothing in this repository is linked into a consumer: a
library that tests against the images talks to them over TCP and UDP and
reads their output, and is not affected by their licences. cpppo's own
`LICENSE` says the same of applications that only talk to it over the
protocols it implements.

The cpppo adapter is a derivative work of the GPL-3.0 cpppo and carries
`SPDX-License-Identifier: GPL-3.0-or-later`; the other adapters are MIT.

## Image `ethernetip-interop-opener`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| OpENer (EIPStackGroup; (c) 2009 Rockwell Automation, Inc.) | master `0e1ab7ee5c272950c0dc8daf7793008d7a518912` (git describe v2.3-591), GitHub archive SHA-256 `5b01333bdc300c1410cf9e02d33a564320b7a6094786a047065a7ec14b21d945` | adapted BSD ("Software distribution license for the EtherNet/IP(TM) communication stack") | https://github.com/EIPStackGroup/OpENer |
| libcap2, iproute2 (Debian packages) | Debian trixie | BSD-3-Clause or GPL-2.0 (libcap), GPL-2.0 (iproute2) | https://www.debian.org |
| Adapter | this repository, `adapters/opener/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence in the image: `/licenses/opener/license.txt`, OpENer's licence as
distributed with the pinned source.

**ODVA clause.** OpENer's licence permits redistribution in source and binary
form with its copyright and trademark notices, and adds that anyone who
implements EtherNet/IP in a product needs ODVA's Terms of Usage for the
EtherNet/IP specification. This image is a test fixture, not a product; if
you build a product from OpENer, that obligation is yours. EtherNet/IP is a
trademark of ODVA, Inc.

## Image `ethernetip-interop-eipscanner`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| EIPScanner ((c) 2019 Aleksey Timin), linked statically | master `12c89a57ec2e7449efb1348be52af10be9ef9701` (1.3.0 + 33 commits), GitHub archive SHA-256 `3efbb95bff91501328b5899dd635cfee61ec11f59f3ad7609812fb65211ddc63` | MIT | https://github.com/nimbuscontrols/EIPScanner |
| GNU C++ library (Debian `libstdc++6`) | Debian trixie | GPL-3.0 with the GCC Runtime Library Exception | https://gcc.gnu.org |
| Adapter | this repository, `adapters/eipscanner/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence in the image: `/licenses/eipscanner/LICENSE`.

## Image `ethernetip-interop-pycomm3`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| pycomm3 ((c) 2020 Ian Ottoway, (c) 2014 Agostino Ruscito) | 1.2.16 (PyPI wheel SHA-256 `88e95c70472154ee32233f76af126526db208830e1e803eb9f621c8bd1d8fcd0`; tag commit `36aeb19b5cb47495df9fd1caa38ce9d2f6d12ea3`) | MIT | https://github.com/ottowayi/pycomm3 |
| Python | 3.14.8 (official `python:3.14.8-slim-trixie` image) | PSF-2.0 | https://www.python.org |
| Adapter | this repository, `adapters/pycomm3/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence in the image: `/licenses/pycomm3/LICENSE`.

## Image `ethernetip-interop-cpppo`

**The image as a whole is under the GNU General Public License version 3**:
it contains cpppo (GPL-3.0) and the cpppo adapter (GPL-3.0-or-later), which
imports it.

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| cpppo ((c) 2013 Hard Consulting Corporation; Dominion Research & Development Corp.) | 5.2.5 (PyPI wheel SHA-256 `0257ba393f1e2bdd06ea712cead740e496c802b3e5275c1995ee2df31f29ed1c`; tag commit `f7c224bbb2389bd26d0088eeb1a93db3a5c4858f`) | GPL-3.0 (cpppo is dual-licensed: GPL-3.0 or a negotiated proprietary licence; used here under the GPL) | https://github.com/pjkundert/cpppo |
| greenery (Sam Hughes, "qntm") | 2.1 (PyPI sdist SHA-256 `759c3bf40b6fe6ef0a6b373230d07470e0e315f32bcdbaf381901b0d8a6f91d1`) | MIT | https://github.com/qntm/greenery |
| Python | 3.14.8 (official `python:3.14.8-slim-trixie` image) | PSF-2.0 | https://www.python.org |
| Adapter | this repository, `adapters/cpppo/` | GPL-3.0-or-later | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licences in the image: `/licenses/cpppo/LICENSE` (cpppo's licensing
statement) and `/licenses/cpppo/COPYING` (the GPL-3.0 text), as distributed in
the wheel; `/licenses/GPL-3.0.txt` (the adapter's licence);
`/licenses/greenery/NOTICE` (greenery's sdist ships no licence file; its
package metadata states the MIT License).

**Corresponding source.** The image contains cpppo and greenery as installed
from PyPI, unmodified: the Python sources are in the image under
`/usr/local/lib/python3.14/site-packages/`, and the pinned versions are
available from PyPI and from the repositories above. The adapter's source is
in this repository at the release tag (`adapters/cpppo/`) and in the image at
`/opt/adapter/cpppo_interop.py`.

## Image `ethernetip-interop-libplctag`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| libplctag ((c) Kyle Hayes and contributors), linked statically into the adapter, and its PLC simulator `ab_server` | v2.7.2, `7712ed7f1ee05ba0ebfca641b3f5c43da52a66ed`, GitHub archive SHA-256 `ed1f7869fa266df1bc266b9d135cd09d53dc392e557edb9986b2e172c84eb752` | MPL-2.0 OR LGPL-2.0-or-later (used under MPL-2.0) | https://github.com/libplctag/libplctag |
| Adapter | this repository, `adapters/libplctag/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licences in the image: `/licenses/libplctag/LICENSE-1.MPL` (also as
`LICENSE`) and `/licenses/libplctag/LICENSE-2.LGPL`. libplctag is built
unmodified; its source, as MPL-2.0 requires for binary distribution, is the
pinned GitHub archive above.

## Image `ethernetip-interop-gologix`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| gologix ((c) 2022 danomagnum), compiled in | v0.44.0-beta, `647a0b87b756d42e37ac0f65c5762dc89036152c` (Go module sum `h1:QrkDcR4ZFmtZ2lhWpdiE+8025nTwO4BulJkPZV+wsp8=`) | MIT | https://github.com/danomagnum/gologix |
| crc16 ((c) 2014 Nick Patavalis), compiled in | v0.0.0-20161013170008-4128ccbe47c3 | BSD-2-Clause | https://github.com/npat-efault/crc16 |
| Adapter | this repository, `adapters/gologix/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licences in the image: `/licenses/gologix/LICENSE`, `/licenses/crc16/LICENSE`.

## Image `ethernetip-interop-pylogix`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| pylogix ((c) Dustin Roeder) | 1.1.6 (PyPI wheel SHA-256 `f181ff0990468c2e7fe0985be190c0bfe9c0d54dc4bb3f28f5a6d76b92cf980f`; tag commit `193b6c7b0c601619223ee53587abd821d3b344c1`), unmodified | Apache-2.0 | https://github.com/dmroeder/pylogix |
| Python | 3.14.8 (official `python:3.14.8-slim-trixie` image) | PSF-2.0 | https://www.python.org |
| Adapter | this repository, `adapters/pylogix/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence in the image: `/licenses/pylogix/LICENSE`.

## Image `ethernetip-interop-etherip`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| etherip ((c) 2012 UT-Battelle, LLC and contributors) | main `ce0688ae51be5652ec200fbe389667b36a638323` (V1.0 + 67 commits), GitHub archive SHA-256 `35ad2cbb710be5bef6cae0b2441a9397534249ddb33e9ac4e1e377b2e6546c7e`, jar built unmodified | EPL-1.0 | https://github.com/ornl-epics/etherip |
| Eclipse Temurin JRE | 21.0.8+9 (official `eclipse-temurin:21.0.8_9-jre-noble` image) | GPL-2.0 with the Classpath Exception | https://adoptium.net |
| Adapter | this repository, `adapters/etherip/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Ubuntu 24.04 (noble) | various; see `/usr/share/doc/*/copyright` in the image | https://ubuntu.com |

Licence in the image: `/licenses/etherip/LICENSE`. The source of the etherip
jar is the pinned archive above.

## Image `ethernetip-interop-eiprs`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| rseip, rseip-cip, rseip-eip, rseip-core (eip-rs, (c) 2021 Joylei), linked statically | main `16e2236afb65acc9c82ebdcfbdcbba72e93b3887` (crate 0.3.1 + 2 commits), GitHub archive SHA-256 `92cccb84822056e23ab93a2d200f11ebf7a3c6813648eaf081d653d2961da77b` | MIT | https://github.com/Joylei/eip-rs |
| Rust crates from crates.io (tokio, bytes, futures, serde_json, smallvec, ...), linked statically | as in `adapters/eiprs/Cargo.lock` | MIT and/or Apache-2.0 | https://crates.io |
| Adapter | this repository, `adapters/eiprs/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licences in the image: `/licenses/eip-rs/LICENSE`, and the crates' licence
files under `/licenses/crates/<crate>-<version>/`.

## Image `ethernetip-interop-nodeeip`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| node-ethernet-ip (npm `ethernet-ip`, (c) 2018-2026 Canaan Seaton) | 2.0.0 (npm integrity `sha512-jGxbu8ifv2qgPzaNKbyFmLaOaefrsg3aVsBWEj3BTWyGSfaVRx/MnkNJuiR/+41GzWgt/LZpYnFGJnAnjO7DCA==`; tag commit `4743b077abd3cc8826d511acde8f890372826e16`); no runtime dependencies | MIT | https://github.com/cmseaton42/node-ethernet-ip |
| Node.js | 24.22.1 (official `node:24.22.1-trixie-slim` image) | MIT and the licences of its components | https://nodejs.org |
| Adapter | this repository, `adapters/nodeeip/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence in the image: `/licenses/node-ethernet-ip/LICENSE`.

## Image `ethernetip-interop-eeip`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| EEIP.NET ((c) 2020 Rossmann Engineering), `EEIP.dll` built unchanged from the sources | master `58b91a0c8e30227b1b97a637635d147a2a9655dc` (1.6.0), GitHub archive SHA-256 `8ad4f3e498b6d02b1c65b8fae498100a9f9f90d6f691423e5c80559c4eceff5b` | MIT | https://github.com/rossmann-engineering/EEIP.NET |
| .NET runtime | 10.0 (`mcr.microsoft.com/dotnet/runtime:10.0`) | MIT and the licences of its components | https://dot.net |
| Adapter | this repository, `adapters/eeip/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Ubuntu 24.04 | various; see `/usr/share/doc/*/copyright` in the image | https://ubuntu.com |

Licence in the image: `/licenses/eeip/LICENSE`.

## Image `ethernetip-interop-lokieip`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| go-ethernet-ip (loki-os), compiled in | v0.0.0-20221229063210-535f79752f63, `535f79752f63a4122c37c7f0f9b95ece01a8f7b4` (Go module sum `h1:kkL3XKGDjH1qNF78xvwJ9+lzbCwy5cCQxtkcNnbVurw=`) | WTFPL | https://github.com/loki-os/go-ethernet-ip |
| Adapter | this repository, `adapters/lokieip/` | MIT | https://github.com/otfabric/ethernetip-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence in the image: `/licenses/go-ethernet-ip/LICENSE`. The upstream
repository also contains a copy of Rockwell's 1756-PM020 manual (PDF); it is
not part of the module's code and not in the image.

## Images in general

Every image also contains `/licenses/LICENSE` (this repository's MIT
licence) and this file. The upstream versions and digests of a release are in
its `manifest.json`; the pins are in [versions.yaml](versions.yaml).
