# Changelog

## [0.18.0](https://github.com/icholy/xagent/compare/v0.17.0...v0.18.0) (2026-05-23)


### Features

* allow accessing different org in different tabs ([#658](https://github.com/icholy/xagent/issues/658)) ([e1ef73d](https://github.com/icholy/xagent/commit/e1ef73d513e59dd25009ee991d26f933f1e7af55))
* auto-archive tasks after configurable timeout ([#655](https://github.com/icholy/xagent/issues/655)) ([93fc393](https://github.com/icholy/xagent/commit/93fc3934da035953e5717ed72c201a16e5931355))
* **runner:** auto-repair stale container network attachments ([#666](https://github.com/icholy/xagent/issues/666)) ([6a894c4](https://github.com/icholy/xagent/commit/6a894c466c0bc83b42fccf9af2d4cc50fd10d79b))
* **runner:** gate child task operations behind workspace scopes ([#672](https://github.com/icholy/xagent/issues/672)) ([204b5dc](https://github.com/icholy/xagent/commit/204b5dc071f10a921e80def79f718916923ae37c))
* **runner:** gate github token issuance behind workspace scopes ([#669](https://github.com/icholy/xagent/issues/669)) ([38521f1](https://github.com/icholy/xagent/commit/38521f1f0a76ea9840a192368bf085d7a861ec54))


### Bug Fixes

* **agent:** only mention child task tools when scope is granted ([#674](https://github.com/icholy/xagent/issues/674)) ([9b1e086](https://github.com/icholy/xagent/commit/9b1e086c20b2252bbe4091d615f2502b02c8cef8))
* explicitly pass org query parameter in all links ([4169031](https://github.com/icholy/xagent/commit/41690317537c2051e8839f85cb39cc965c441b49))
* guard nil github server in CreateGitHubToken ([#667](https://github.com/icholy/xagent/issues/667)) ([a7183ec](https://github.com/icholy/xagent/commit/a7183ec0b3f2c1e5f78fda0e5812d8fdd2803b0c))
* normalize escaped newlines in GitHub App private key ([#665](https://github.com/icholy/xagent/issues/665)) ([163015d](https://github.com/icholy/xagent/commit/163015def2a6dd932c54d01991f610ce9c9aea10))
* parse github private key in githubserver.New ([#663](https://github.com/icholy/xagent/issues/663)) ([64ff92d](https://github.com/icholy/xagent/commit/64ff92d13f020486e158aad9357092389b231eab)), closes [#660](https://github.com/icholy/xagent/issues/660)
* pass org query param in navigate/redirect calls ([#664](https://github.com/icholy/xagent/issues/664)) ([f9a645b](https://github.com/icholy/xagent/commit/f9a645b788940f759e75b1ce43e7b320c7c0db36))
* replace dotenv output format with jq expression to fix newline issue ([0ab9366](https://github.com/icholy/xagent/commit/0ab936688a43632efc6b75eb2b23ba9164a6d589))


### Miscellaneous

* add playwrite mcp dir to .gitignore ([247ac84](https://github.com/icholy/xagent/commit/247ac842b173e4c7d8f70ba765568340353778bd))
* **apiauth:** inline validateAppToken into authenticate ([#676](https://github.com/icholy/xagent/issues/676)) ([62d1b7f](https://github.com/icholy/xagent/commit/62d1b7f03420cbede1983e63557ca51469015050))
* **deps:** update dependency @tanstack/router-vite-plugin to v1.167.5 ([#671](https://github.com/icholy/xagent/issues/671)) ([ff93dc8](https://github.com/icholy/xagent/commit/ff93dc8f825d45f9f7ad04655cfea00a4aa96d78))
* **deps:** update tanstack-router monorepo ([#659](https://github.com/icholy/xagent/issues/659)) ([95324ca](https://github.com/icholy/xagent/commit/95324caadced5c382e817b691997c4b2e75d535b))
* drop X-Auth-Type header dispatch ([#673](https://github.com/icholy/xagent/issues/673)) ([a5ba15e](https://github.com/icholy/xagent/commit/a5ba15ef55aa4aaae4f006e10f235f27b4ad654d))
* move implemented proposals to accepted ([6efb5ab](https://github.com/icholy/xagent/commit/6efb5ab422a5f640ed9d2859f62e24f2292e7524))
* move simplify-bearer-auth-dispatch proposal to accepted ([#675](https://github.com/icholy/xagent/issues/675)) ([0260c86](https://github.com/icholy/xagent/commit/0260c862775218fa935becd503f1fd98d517b595))
* remove debug log ([1b2c72a](https://github.com/icholy/xagent/commit/1b2c72aa67306fabdf997991170016f9a0e184c4))
* remove debug log ([cd8aabf](https://github.com/icholy/xagent/commit/cd8aabf60378417e27c24d14b0f3cce5b386b210))
* remove xagent setup command and device flow ([#668](https://github.com/icholy/xagent/issues/668)) ([ec53ce2](https://github.com/icholy/xagent/commit/ec53ce2008ee06f8e85a28bf47eaeb706fc62e1c))
* remove zitadel bearer middleware from apiauth ([#670](https://github.com/icholy/xagent/issues/670)) ([d4e6bea](https://github.com/icholy/xagent/commit/d4e6beae63feaa9e44f82d3adf2e21d551efdd75))
* run formatter ([6efc665](https://github.com/icholy/xagent/commit/6efc6652cf62eb7f92712ae3393029b92af1cf3b))

## [0.17.0](https://github.com/icholy/xagent/compare/v0.16.0...v0.17.0) (2026-05-23)


### Features

* add get_github_token agent MCP tool ([#651](https://github.com/icholy/xagent/issues/651)) ([7f73c36](https://github.com/icholy/xagent/commit/7f73c365f72e8ba5c37c5da09d6bb904bfd658b7))
* add xagent tool github-mcp subcommand ([#653](https://github.com/icholy/xagent/issues/653)) ([06a9d3a](https://github.com/icholy/xagent/commit/06a9d3a658af85b883f5ccd5aba94ab36fb35a27))


### Bug Fixes

* **deps:** update module github.com/urfave/cli/v3 to v3.9.0 ([#648](https://github.com/icholy/xagent/issues/648)) ([cc74fb9](https://github.com/icholy/xagent/commit/cc74fb9e06317d92ea4508096ddeaf7e5abaec91))
* inject release tag as version at build time ([48df1dd](https://github.com/icholy/xagent/commit/48df1dd37f9aab1762bf00a1a77aa5f16ffce211))
* use the transport onOrgChange to update the org query ([63596c2](https://github.com/icholy/xagent/commit/63596c27ea05eba62a30976a716dbd80c904150b))


### Miscellaneous

* **deps:** update tanstack-router monorepo ([#650](https://github.com/icholy/xagent/issues/650)) ([33959d7](https://github.com/icholy/xagent/commit/33959d7d4aecb862851bb7bbcaf81e0efc61dc79))
* group in-container helpers under a tool namespace ([8d128d0](https://github.com/icholy/xagent/commit/8d128d0ae596ed5536ff5e12401ce40b57dbd25d))
* use gotest.tools assert in mcpswap tests ([#652](https://github.com/icholy/xagent/issues/652)) ([5c4cea1](https://github.com/icholy/xagent/commit/5c4cea1cebff1aea3d67ff3d5f9c5d199c69b060))
* vendor mcpswap single-upstream MCP adapter ([14b5be6](https://github.com/icholy/xagent/commit/14b5be6533b2797302918f7e269107465f63e480))

## [0.16.0](https://github.com/icholy/xagent/compare/v0.15.4...v0.16.0) (2026-05-23)


### Features

* add ?org= query param for deep-link org routing ([#643](https://github.com/icholy/xagent/issues/643)) ([8b1bb2a](https://github.com/icholy/xagent/commit/8b1bb2a1bbcce172a4793407cd07633336aa342d))
* show server version in settings page footer ([#646](https://github.com/icholy/xagent/issues/646)) ([44eb88e](https://github.com/icholy/xagent/commit/44eb88ea39294b612492fa5d3bb592aeb2a71974))


### Bug Fixes

* **deps:** update module github.com/docker/cli to v29.5.0+incompatible ([#625](https://github.com/icholy/xagent/issues/625)) ([807045e](https://github.com/icholy/xagent/commit/807045eee8c68786d1c82214c87777c66bb504af))


### Miscellaneous

* **deps:** update module github.com/matryer/moq to v0.7.1 ([#630](https://github.com/icholy/xagent/issues/630)) ([de997e4](https://github.com/icholy/xagent/commit/de997e4c38f1476d58bfe9ae9d5431ef1b393ef6))
* **deps:** update module github.com/sqlc-dev/sqlc to v1.31.1 ([#631](https://github.com/icholy/xagent/issues/631)) ([7c7b543](https://github.com/icholy/xagent/commit/7c7b543c05dcb9d5db7886f7ffa6fbf3fb55026e))
* **deps:** update module golang.org/x/tools to v0.45.0 ([#632](https://github.com/icholy/xagent/issues/632)) ([d14c826](https://github.com/icholy/xagent/commit/d14c82696dde684c66886f257c7717c27ea4a0f4))
* **deps:** update tailwindcss monorepo to v4.3.0 ([#640](https://github.com/icholy/xagent/issues/640)) ([fe3432d](https://github.com/icholy/xagent/commit/fe3432d1314ce3b6edf538d20d22eeca48fb051a))
* **deps:** update tanstack-query monorepo to v5.100.10 ([#642](https://github.com/icholy/xagent/issues/642)) ([bfd5568](https://github.com/icholy/xagent/commit/bfd55687218a6439d6682c162098a4bd57a6dce4))
* **deps:** update tanstack-router monorepo ([#644](https://github.com/icholy/xagent/issues/644)) ([607b2bf](https://github.com/icholy/xagent/commit/607b2bffed25dcd56873a1f27cbea14216fe6c8e))
* fix linting errors ([08d90dd](https://github.com/icholy/xagent/commit/08d90dd6723a6214d339e6ebad185db3c8d0cbbf))
* remind agents to run pnpm lint on webui changes ([7ec88e2](https://github.com/icholy/xagent/commit/7ec88e2d2c62034f853563ed287ad7e39ffde69a))
* run eslint on webui PRs ([74d79bf](https://github.com/icholy/xagent/commit/74d79bf052b9d59262a2cc347f2018afdfa46812))
* schedule docker/cli updates weekly ([#641](https://github.com/icholy/xagent/issues/641)) ([6bd5ad9](https://github.com/icholy/xagent/commit/6bd5ad92ba6249c90d99cb762734d6f9dd71a27f))
* throttle @typescript/native-preview to weekly updates ([#628](https://github.com/icholy/xagent/issues/628)) ([f205845](https://github.com/icholy/xagent/commit/f20584523651067bcbfe878134a449df711db3ed))
* **webui:** add prettier ([#645](https://github.com/icholy/xagent/issues/645)) ([08b3ab9](https://github.com/icholy/xagent/commit/08b3ab943e8689332e961eb5a172f1250f84a28a))

## [0.15.4](https://github.com/icholy/xagent/compare/v0.15.3...v0.15.4) (2026-05-21)


### Bug Fixes

* return 401 for invalid Bearer tokens ([#624](https://github.com/icholy/xagent/issues/624)) ([de01986](https://github.com/icholy/xagent/commit/de019863e56d5a73da3f44d0b192f991238ab593))


### Miscellaneous

* **deps:** update dependency @typescript/native-preview to v7.0.0-dev.20260514.1 ([#620](https://github.com/icholy/xagent/issues/620)) ([98e6611](https://github.com/icholy/xagent/commit/98e6611ba0965ba347843725e165939a348d8014))

## [0.15.3](https://github.com/icholy/xagent/compare/v0.15.2...v0.15.3) (2026-05-20)


### Bug Fixes

* remove MCP hello middleware that broke n8n clients ([#619](https://github.com/icholy/xagent/issues/619)) ([359bd77](https://github.com/icholy/xagent/commit/359bd77a9ceeea8148840c42c811eaf2469d35fe))
* remove nested code tags from MCP landing page pre blocks ([#617](https://github.com/icholy/xagent/issues/617)) ([bf9c49b](https://github.com/icholy/xagent/commit/bf9c49ba6a54a3225fde8184a3fd9cbb2aabc713))

## [0.15.2](https://github.com/icholy/xagent/compare/v0.15.1...v0.15.2) (2026-05-20)


### Bug Fixes

* scope runner docker operations by runner id ([#615](https://github.com/icholy/xagent/issues/615)) ([63b0f28](https://github.com/icholy/xagent/commit/63b0f289f860d1e296a1ffbae59ceb966ef77945))

## [0.15.1](https://github.com/icholy/xagent/compare/v0.15.0...v0.15.1) (2026-05-20)


### Bug Fixes

* **deps:** update module github.com/docker/cli to v29.4.3+incompatible ([#611](https://github.com/icholy/xagent/issues/611)) ([7f8c513](https://github.com/icholy/xagent/commit/7f8c513c5d83060a9cfeee6c871e9b886b331e70))
* move agent unix socket to /xagent.sock ([#614](https://github.com/icholy/xagent/issues/614)) ([f90ad22](https://github.com/icholy/xagent/commit/f90ad22df40db5146855d0de47814288f8b79d20))


### Miscellaneous

* **deps:** update dependency @types/node to v24.12.4 ([#609](https://github.com/icholy/xagent/issues/609)) ([236f973](https://github.com/icholy/xagent/commit/236f97305dea3da404a9fa9be37b52ebe833d87e))
* **deps:** update dependency @typescript/native-preview to v7.0.0-dev.20260512.1 ([#612](https://github.com/icholy/xagent/issues/612)) ([5762cc5](https://github.com/icholy/xagent/commit/5762cc56ffd4d8a9cee583fb7883f84d7216977b))
* **deps:** update dependency @typescript/native-preview to v7.0.0-dev.20260513.1 ([#613](https://github.com/icholy/xagent/issues/613)) ([00d81b1](https://github.com/icholy/xagent/commit/00d81b11b77a744f14e775195f9b52afcf7354d0))
* **deps:** update dependency tailwind-merge to v3.6.0 ([#606](https://github.com/icholy/xagent/issues/606)) ([58ba3f4](https://github.com/icholy/xagent/commit/58ba3f43e6f73418cd1cccc9a461df72932fb5e9))
* **deps:** update dependency typescript-eslint to v8.59.3 ([#608](https://github.com/icholy/xagent/issues/608)) ([e6cbdfe](https://github.com/icholy/xagent/commit/e6cbdfe17752ce14610554f997519985d7d55798))
* **deps:** update module github.com/bufbuild/buf to v1.69.0 ([#610](https://github.com/icholy/xagent/issues/610)) ([1f0160f](https://github.com/icholy/xagent/commit/1f0160f24a3d25a5901222793eca5f464f581fd9))

## [0.15.0](https://github.com/icholy/xagent/compare/v0.14.1...v0.15.0) (2026-05-18)


### Features

* add CreateGitHubToken RPC for GitHub App installation tokens ([#594](https://github.com/icholy/xagent/issues/594)) ([b8b9c3d](https://github.com/icholy/xagent/commit/b8b9c3dda544f080f09b3dfc734a146111fe148a))
* add git-credential subcommand for GitHub App installation tokens ([#603](https://github.com/icholy/xagent/issues/603)) ([2e0fc6a](https://github.com/icholy/xagent/commit/2e0fc6abb51484e96488f25a210205b836bc922f))


### Bug Fixes

* wire git-credential helper through agent filter and env token ([#605](https://github.com/icholy/xagent/issues/605)) ([dc3369e](https://github.com/icholy/xagent/commit/dc3369e2ef05ebe7eac6932261b79968a9464cc3))


### Miscellaneous

* **deps:** update dependency @vitejs/plugin-react to v5.2.0 ([#599](https://github.com/icholy/xagent/issues/599)) ([595fb8d](https://github.com/icholy/xagent/commit/595fb8d324431324e7e27f0d80ff1cca0ba73e6d))
* **deps:** update dependency eslint-plugin-react-refresh to ^0.5.0 ([#604](https://github.com/icholy/xagent/issues/604)) ([8e6e05e](https://github.com/icholy/xagent/commit/8e6e05e6551fdc16fe68ff484667e7d5a879e792))

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
