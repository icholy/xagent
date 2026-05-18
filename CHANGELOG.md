# Changelog

## [0.14.1](https://github.com/icholy/xagent/compare/v0.14.0...v0.14.1) (2026-05-18)


### Bug Fixes

* change GitHub App install label to 'Installed' in org settings ([#598](https://github.com/icholy/xagent/issues/598)) ([8d09553](https://github.com/icholy/xagent/commit/8d09553e4daf99691336b9ffbc43b090520db4c5))


### Miscellaneous

* **deps:** update dependency @bufbuild/protoc-gen-es to v2.12.0 ([#592](https://github.com/icholy/xagent/issues/592)) ([021651c](https://github.com/icholy/xagent/commit/021651c28ff811cc198ff5e3efd11876d65a923c))
* **deps:** update dependency @types/node to v24.12.3 ([#595](https://github.com/icholy/xagent/issues/595)) ([f2625ab](https://github.com/icholy/xagent/commit/f2625abd89b62eefece80971a00eeaf102bb02a2))
* **deps:** update dependency @typescript/native-preview to v7.0.0-dev.20260511.1 ([#596](https://github.com/icholy/xagent/issues/596)) ([98d7caf](https://github.com/icholy/xagent/commit/98d7caf14af37e4e86c1e9c6055e88ead7b47671))

## [0.14.0](https://github.com/icholy/xagent/compare/v0.13.2...v0.14.0) (2026-05-17)


### Features

* show GitHub App install state in org settings ([#593](https://github.com/icholy/xagent/issues/593)) ([1489532](https://github.com/icholy/xagent/commit/148953258282b1ea28f54841be1d0f7517b73321))


### Bug Fixes

* check out tag in deploy job so flyctl sees fly.toml ([28abffb](https://github.com/icholy/xagent/commit/28abffbadfb29230cc3ddbb4861589099057b8a0))


### Miscellaneous

* **deps:** update dependency @bufbuild/protobuf to v2.12.0 ([#591](https://github.com/icholy/xagent/issues/591)) ([4209bad](https://github.com/icholy/xagent/commit/4209bad2cb810337cfb4e15765466c46e7ddd401))
* extract driver logic into agent package ([#590](https://github.com/icholy/xagent/issues/590)) ([68d6b37](https://github.com/icholy/xagent/commit/68d6b37f1299b559d7541a3a77e25f645dcb3e27))
* keep failed deploys visible as failures in deployment history ([2f12982](https://github.com/icholy/xagent/commit/2f12982e0251a95ec867b6532909eaba48ebf2f3))
* simplify github setup confirmation screen ([62e58ed](https://github.com/icholy/xagent/commit/62e58edbbf96088485b99a1246c0e55e2f8ea170))

## [0.13.2](https://github.com/icholy/xagent/compare/v0.13.1...v0.13.2) (2026-05-17)


### Bug Fixes

* bump dockerfile go to 1.26 and node to 25 ([c915954](https://github.com/icholy/xagent/commit/c9159541ea5f66acbb6e1c65f9eeaf19d2ba3b30))

## [0.13.1](https://github.com/icholy/xagent/compare/v0.13.0...v0.13.1) (2026-05-17)


### Miscellaneous

* allow any scope in conform conventional commit policy ([a7a57fd](https://github.com/icholy/xagent/commit/a7a57fd6e75e1a008722083dc14ea36e5c18b0c3))
* combine release-please into release.yml, split deploy ([5b38cc4](https://github.com/icholy/xagent/commit/5b38cc4666207005231cd3e9ab12e342902289ff))
* **deps:** update alpine docker tag to v3.23 ([#585](https://github.com/icholy/xagent/issues/585)) ([ff44217](https://github.com/icholy/xagent/commit/ff442173fe1d4d8258dd210fade2e7644543117d))
* **deps:** update dependency @bufbuild/buf to v1.69.0 ([#586](https://github.com/icholy/xagent/issues/586)) ([e97ecb7](https://github.com/icholy/xagent/commit/e97ecb73f721cfb90639ed7296766ebe50193446))
* **deps:** update dependency @typescript/native-preview to v7.0.0-dev.20260510.1 ([#583](https://github.com/icholy/xagent/issues/583)) ([694c435](https://github.com/icholy/xagent/commit/694c43556dea8b2524b9230ddf9c131f51642aad))
* **deps:** update react monorepo ([#584](https://github.com/icholy/xagent/issues/584)) ([eda2420](https://github.com/icholy/xagent/commit/eda2420abc0403ecc51f93998d498de7fd10ebc4))
* drop installation from manifest default_events ([df0a90a](https://github.com/icholy/xagent/commit/df0a90ae9868f35520575b9da9f14807bceac88b))
* group non-release commit types under Miscellaneous in changelog ([4886bee](https://github.com/icholy/xagent/commit/4886bee411be974b2be192682987a5e8db8fedf6))
* let renovate open PRs anytime, limit to 3 concurrent ([27062b7](https://github.com/icholy/xagent/commit/27062b7f64459d49729bfd62c518101626711300))

## [0.13.0](https://github.com/icholy/xagent/compare/v0.12.1...v0.13.0) (2026-05-17)


### Features

* allow driver to submit own runner events via AgentFilter ([#582](https://github.com/icholy/xagent/issues/582)) ([96e41df](https://github.com/icholy/xagent/commit/96e41df372069e3d850be60de85c0f814034b525))
* store GitHub App installation id on org ([#579](https://github.com/icholy/xagent/issues/579)) ([8b1ae59](https://github.com/icholy/xagent/commit/8b1ae59ee171a7673bfc16b407014d7ec6aa7626))


### Bug Fixes

* add sslmode=disable to XAGENT_DATABASE_URL ([5b5116a](https://github.com/icholy/xagent/commit/5b5116a642c39b2bf810cbf21598edee00764a3f))
