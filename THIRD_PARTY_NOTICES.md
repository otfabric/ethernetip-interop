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
| Image `ethernetip-interop-opener` | MIT and the OpENer licence (adapted BSD, see below), and the licences of the Debian packages |
| Image `ethernetip-interop-eipscanner` | MIT, and the licences of the Debian packages |
| Image `ethernetip-interop-pycomm3` | MIT, and the Python licence |
| Image `ethernetip-interop-cpppo` | **GPL-3.0 as a whole** (it contains cpppo and the GPL adapter), and the licences of its components |

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
| OpENer (EIPStackGroup; (c) 2009 Rockwell Automation, Inc.) | master `d8820ef41ce30dda06d213438ba81c81de33ffe2` (git describe v2.3-588), GitHub archive SHA-256 `6c34be6bfe294351a69544722079751f96cdc24ac1a43337dd3e22d843f5b95b` | adapted BSD ("Software distribution license for the EtherNet/IP(TM) communication stack") | https://github.com/EIPStackGroup/OpENer |
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

## Images in general

Every image also contains `/licenses/LICENSE` (this repository's MIT
licence) and this file. The upstream versions and digests of a release are in
its `manifest.json`; the pins are in [versions.yaml](versions.yaml).
