# Changelog

## [0.22.2](https://github.com/icholy/xagent/compare/v0.22.1...v0.22.2) (2026-05-31)


### Bug Fixes

* copy pnpm-workspace.yaml before webui install in server image ([155117b](https://github.com/icholy/xagent/commit/155117b7ba7b50d7921260cdda1a349a7c081878))


### Miscellaneous

* **deps:** update docker/setup-buildx-action action to v4 ([#764](https://github.com/icholy/xagent/issues/764)) ([acc1d80](https://github.com/icholy/xagent/commit/acc1d8016bef00b37f201ba5db1c568792c2ed77))
* **deps:** update docker/setup-qemu-action action to v4 ([#767](https://github.com/icholy/xagent/issues/767)) ([e9de22f](https://github.com/icholy/xagent/commit/e9de22f001d7bc31ff0c22f749ddef0cd52f03bb))

## [0.22.1](https://github.com/icholy/xagent/compare/v0.22.0...v0.22.1) (2026-05-31)


### Bug Fixes

* install postgresql-client-17 from Debian trixie main ([0437292](https://github.com/icholy/xagent/commit/043729212aaf2600762223288275d86b34ebab60))

## [0.22.0](https://github.com/icholy/xagent/compare/v0.21.0...v0.22.0) (2026-05-31)


### Features

* add assignment-triggered routing rules for GitHub ([#761](https://github.com/icholy/xagent/issues/761)) ([4478868](https://github.com/icholy/xagent/commit/4478868642ce0c55acf50763580b783031a189a0))
* add URL-prefix matching to routing rules ([#763](https://github.com/icholy/xagent/issues/763)) ([cf68f57](https://github.com/icholy/xagent/commit/cf68f57ebe3687dd390263e3dc420ddc5330776d)), closes [#742](https://github.com/icholy/xagent/issues/742)
* **mcp:** add archive_task tool to user-facing MCP server ([#744](https://github.com/icholy/xagent/issues/744)) ([e8c333f](https://github.com/icholy/xagent/commit/e8c333f2e441d739d981ba68701cbe6127d2658c))
* **server:** notify agent channel when task is archived ([#747](https://github.com/icholy/xagent/issues/747)) ([3e663f0](https://github.com/icholy/xagent/commit/3e663f04f8a36d5a45989eebfb5e7eba9eafc05c))
* support routing rules that create tasks ([#746](https://github.com/icholy/xagent/issues/746)) ([8098eb7](https://github.com/icholy/xagent/commit/8098eb7707643232c8e9b658d065a5791bcdbf35))
* **webui:** allow reordering routing rules ([#754](https://github.com/icholy/xagent/issues/754)) ([b27297b](https://github.com/icholy/xagent/commit/b27297b9f72e34460b359b5b7be99ae64db799ef)), closes [#717](https://github.com/icholy/xagent/issues/717)
* **webui:** wire CreateTaskAction into routing-rule editor ([#749](https://github.com/icholy/xagent/issues/749)) ([b0cd154](https://github.com/icholy/xagent/commit/b0cd154bb4424a968c30e4f1a6401630322465e7)), closes [#717](https://github.com/icholy/xagent/issues/717)


### Bug Fixes

* **deps:** update module github.com/xsam/otelsql to v0.42.0 ([#726](https://github.com/icholy/xagent/issues/726)) ([33c0645](https://github.com/icholy/xagent/commit/33c064592e5433b6920b395430cffc6ae109bdfa))
* **deps:** update module github.com/zitadel/oidc/v3 to v3.47.5 ([#728](https://github.com/icholy/xagent/issues/728)) ([c528474](https://github.com/icholy/xagent/commit/c52847455b0ab23e8edf2b28fb17744e8ff4fb14))
* **eventrouter:** suppress wake notifications when no transition ([#748](https://github.com/icholy/xagent/issues/748)) ([caabd69](https://github.com/icholy/xagent/commit/caabd69414d8af8c9430ce96ca259716958707db))
* **mcp:** marshal channel meta as empty object when nil ([5ad5eb7](https://github.com/icholy/xagent/commit/5ad5eb702a03cdf5b8d92b66016fe55bffea7c6d))
* resume setup from failed command ([#762](https://github.com/icholy/xagent/issues/762)) ([5cc04b9](https://github.com/icholy/xagent/commit/5cc04b903981feb8ce3603a27c9f3593fcef41ee))
* **server:** there's always a url ([61577cb](https://github.com/icholy/xagent/commit/61577cbb4e06bd5bf6baaa82da17e4aa6cd0043f))
* switch mise pnpm backend to npm and bump Dockerfile to v11 ([#755](https://github.com/icholy/xagent/issues/755)) ([f290563](https://github.com/icholy/xagent/commit/f29056304add99054863b0d244acae8efc1ccd0f))
* **webui:** fold routing-rules hint into card description ([#759](https://github.com/icholy/xagent/issues/759)) ([23c69bb](https://github.com/icholy/xagent/commit/23c69bbe8cc8562c981ed9417cf9f88883cb5bc1))


### Miscellaneous

* add notification-handling guidance to orchestrator skill ([c3fcde6](https://github.com/icholy/xagent/commit/c3fcde6d1af3bf23ccedddca1cc606f4c6eabaab))
* add Task.IsTerminal() helper ([#756](https://github.com/icholy/xagent/issues/756)) ([40cbb12](https://github.com/icholy/xagent/commit/40cbb12f4d1545697223a566391ebc37de3a307a))
* **deps:** update debian docker tag to v13 ([#731](https://github.com/icholy/xagent/issues/731)) ([709af15](https://github.com/icholy/xagent/commit/709af156c8bbef6aaa9d0065cd0c6d57b7a8aac6))
* **deps:** update dependency @vitejs/plugin-react to v6 ([#732](https://github.com/icholy/xagent/issues/732)) ([bb7865b](https://github.com/icholy/xagent/commit/bb7865b11080a82b1a7c2728705a7c93e1eeccee))
* **deps:** update dependency globals to v17 ([#734](https://github.com/icholy/xagent/issues/734)) ([1eeb5a6](https://github.com/icholy/xagent/commit/1eeb5a6fed2f9aceb22ad035d7eb9a2b0d502d64))
* **deps:** update dependency node to v24 ([#735](https://github.com/icholy/xagent/issues/735)) ([1872a96](https://github.com/icholy/xagent/commit/1872a963ae103c3b70218ec37a533e0fd595d004))
* **deps:** update dependency pnpm to v11 ([#740](https://github.com/icholy/xagent/issues/740)) ([c3a0e77](https://github.com/icholy/xagent/commit/c3a0e7752517934e20704fa9bb741ff820f7d539))
* **deps:** update dependency typescript to v6 ([#750](https://github.com/icholy/xagent/issues/750)) ([c4e59c7](https://github.com/icholy/xagent/commit/c4e59c7647646ac7adbbea41aa9ede10dae2c97c))
* **deps:** update docker/build-push-action action to v7 ([#752](https://github.com/icholy/xagent/issues/752)) ([73df7ee](https://github.com/icholy/xagent/commit/73df7ee6165fa2204dd5d6d3dce86c02618eab40))
* **deps:** update docker/login-action action to v4 ([#757](https://github.com/icholy/xagent/issues/757)) ([96d3a8a](https://github.com/icholy/xagent/commit/96d3a8ab1204a56a418ec3a9a6de55582f08e147))
* **deps:** update docker/metadata-action action to v6 ([#758](https://github.com/icholy/xagent/issues/758)) ([66234ff](https://github.com/icholy/xagent/commit/66234ffda4c95fb50c22470bc9c868f52cb918cd))
* gate notification publishing on Notification.Ignore ([#753](https://github.com/icholy/xagent/issues/753)) ([64af1ee](https://github.com/icholy/xagent/commit/64af1ee5b22288f90a8c775573a5978a4b1398ef))
* move implemented proposals out of draft ([4d78311](https://github.com/icholy/xagent/commit/4d78311c104655de55543ecfe4075c1ca2b32fe2))
* **webui:** make routing-rule form source-aware ([#737](https://github.com/icholy/xagent/issues/737)) ([d92122d](https://github.com/icholy/xagent/commit/d92122d508f4f3c505c48a914cdbd9865b859035))
* **webui:** move routing rule editor to dedicated routes ([#730](https://github.com/icholy/xagent/issues/730)) ([8c48b68](https://github.com/icholy/xagent/commit/8c48b683746e338f6a706935f621e2f93b0e52e7))
* **webui:** promote Events to top-level nav page ([#733](https://github.com/icholy/xagent/issues/733)) ([6d831c9](https://github.com/icholy/xagent/commit/6d831c9852598b13f8f7a2116e050a5d51423dd2))

## [0.21.0](https://github.com/icholy/xagent/compare/v0.20.0...v0.21.0) (2026-05-30)


### Features

* **mcp:** channel-message-gated notifications ([#725](https://github.com/icholy/xagent/issues/725)) ([88720f0](https://github.com/icholy/xagent/commit/88720f0860448433c3d5e309f8f0b18b2f5dccce))


### Bug Fixes

* **deps:** update module connectrpc.com/connect to v1.20.0 ([#723](https://github.com/icholy/xagent/issues/723)) ([ceccde5](https://github.com/icholy/xagent/commit/ceccde558a84d583fc680d6a30d30153043a8d10))
* **deps:** update module github.com/modelcontextprotocol/go-sdk to v1.6.1 ([#724](https://github.com/icholy/xagent/issues/724)) ([9648682](https://github.com/icholy/xagent/commit/9648682ef47b6c1dbace485c9bc6d52b524c6e65))
* **mcp:** suppress self-notifications in the bridge ([#719](https://github.com/icholy/xagent/issues/719)) ([ed23ade](https://github.com/icholy/xagent/commit/ed23aded75537056256537bb1addc9f3dfed9880))


### Miscellaneous

* **deps:** update tanstack-query monorepo to v5.100.14 ([#715](https://github.com/icholy/xagent/issues/715)) ([1a45dd8](https://github.com/icholy/xagent/commit/1a45dd84e0b3ef02c0a44bb3fda8d8f05bc3ba5d))
* keep orchestrator task instructions lean ([6b320a5](https://github.com/icholy/xagent/commit/6b320a58ccf43cb84c4cbf12d9f3955d0c269967))
* throttle tanstack renovate updates to monthly ([#721](https://github.com/icholy/xagent/issues/721)) ([e372f49](https://github.com/icholy/xagent/commit/e372f49d721e87106dba4f0d6215d054e60a58b4))

## [0.20.0](https://github.com/icholy/xagent/compare/v0.19.1...v0.20.0) (2026-05-30)


### Features

* add xagent mcp local stdio command ([#708](https://github.com/icholy/xagent/issues/708)) ([5e34cc0](https://github.com/icholy/xagent/commit/5e34cc0261dab439dd9209bfe81abb89e87e785b))
* **mcp:** push task changes as Claude Code channel events ([#713](https://github.com/icholy/xagent/issues/713)) ([2b74513](https://github.com/icholy/xagent/commit/2b7451306d12d3ed777ff66e7e982d93f38bd58c))
* **webui:** move events into a settings tab ([#710](https://github.com/icholy/xagent/issues/710)) ([346835f](https://github.com/icholy/xagent/commit/346835f1ddcee8c3965c7579d19df60131f5d740))


### Bug Fixes

* **runner:** wake main loop when a concurrency slot frees ([#711](https://github.com/icholy/xagent/issues/711)) ([b3c9be3](https://github.com/icholy/xagent/commit/b3c9be374b57e670c9ce4d6b757359c215cb0e8b))
* **webui:** remove Recent Events card description ([b701b62](https://github.com/icholy/xagent/commit/b701b622154f6ece7707d9d265ec064473df72f6))


### Miscellaneous

* add xagent-orchestrator skill ([ec4c2ac](https://github.com/icholy/xagent/commit/ec4c2acd988112a12848c7f91df78eec17af013d))
* extract shared SSE notification subscriber into xagentclient ([#714](https://github.com/icholy/xagent/issues/714)) ([361109a](https://github.com/icholy/xagent/commit/361109a28c2dbcdfec9bae149b5c2dd58d9f34ca))
* extract wakeup.Chan coalescing wake-up signal ([#712](https://github.com/icholy/xagent/issues/712)) ([b11174d](https://github.com/icholy/xagent/commit/b11174d48dc639a1694fe972303f5a05f9306920))
* move agent mcp server under tool subcommand ([#705](https://github.com/icholy/xagent/issues/705)) ([12a4fdb](https://github.com/icholy/xagent/commit/12a4fdb09c62d3881ff08f078e67d2a2356d4672))
* organize the proposals ([e90ef2f](https://github.com/icholy/xagent/commit/e90ef2f81d69edc4982dd397bc1e327b7c6fee03))
* remove redundant TestRunnerWakeChannel ([a7a8cc8](https://github.com/icholy/xagent/commit/a7a8cc892cfd8c78641e1b8b544fc16544747497))
* rework channels proposal around local mcp bridge ([#706](https://github.com/icholy/xagent/issues/706)) ([bed15d1](https://github.com/icholy/xagent/commit/bed15d1f1ef2b5577c8bccb314337f8332d02052))

## [0.19.1](https://github.com/icholy/xagent/compare/v0.19.0...v0.19.1) (2026-05-30)


### Miscellaneous

* **deps:** update dependency @types/react to v19.2.15 ([#699](https://github.com/icholy/xagent/issues/699)) ([5f79162](https://github.com/icholy/xagent/commit/5f79162ecf6cfa9a1272c80b05e6de31f85618b0))
* **deps:** update dependency date-fns to v4.2.1 ([#700](https://github.com/icholy/xagent/issues/700)) ([d3ce37a](https://github.com/icholy/xagent/commit/d3ce37a847e40678a63d34d6214744bca37ea690))
* **deps:** update dependency date-fns to v4.3.0 ([#702](https://github.com/icholy/xagent/issues/702)) ([d858aba](https://github.com/icholy/xagent/commit/d858ababbb5c2dcb87e3b2d454d2e254da2580b1))
* **deps:** update dependency typescript-eslint to v8.59.4 ([#695](https://github.com/icholy/xagent/issues/695)) ([d857057](https://github.com/icholy/xagent/commit/d8570577de79d141c3bb86381aa867063b18d104))
* **deps:** update tanstack-query monorepo to v5.100.11 ([#696](https://github.com/icholy/xagent/issues/696)) ([1b0943a](https://github.com/icholy/xagent/commit/1b0943a47f3757ac508b4467f13196b5867bdb3c))
* **deps:** update tanstack-query monorepo to v5.100.13 ([#701](https://github.com/icholy/xagent/issues/701)) ([81fd1ac](https://github.com/icholy/xagent/commit/81fd1ac0ea0c34f1f93933ec16baac25496ed2f3))
* **deps:** update tanstack-router monorepo ([#689](https://github.com/icholy/xagent/issues/689)) ([994c5ea](https://github.com/icholy/xagent/commit/994c5ea6f6f59536f90dee0af248e9cb7f35cdfe))
* **deps:** update tanstack-router monorepo ([#697](https://github.com/icholy/xagent/issues/697)) ([47c9fd8](https://github.com/icholy/xagent/commit/47c9fd8e6ba603ff6b621f2305b2869c899a9bd2))
* **deps:** update tanstack-router monorepo ([#698](https://github.com/icholy/xagent/issues/698)) ([bc36594](https://github.com/icholy/xagent/commit/bc36594c3f93e303f0f6757b78d5c8cf9ac9992f))

## [0.19.0](https://github.com/icholy/xagent/compare/v0.18.0...v0.19.0) (2026-05-24)


### Features

* **runner:** wake on SSE task notifications ([#680](https://github.com/icholy/xagent/issues/680)) ([ec664dd](https://github.com/icholy/xagent/commit/ec664ddc2699994a6b1aaf7a67c5a6c06667dc02))
* **server:** route actionable task notifications to runners ([#677](https://github.com/icholy/xagent/issues/677)) ([31bcecf](https://github.com/icholy/xagent/commit/31bcecf257f4036c6697976c254bdbdbfc8b6d19))


### Bug Fixes

* **runner:** bind-mount socket parent dir so it survives runner restart ([#686](https://github.com/icholy/xagent/issues/686)) ([e00b186](https://github.com/icholy/xagent/commit/e00b18697ec886d76106ea395eaa8f8a189946b1))
* **store:** write timestamps in UTC ([#679](https://github.com/icholy/xagent/issues/679)) ([5db29d5](https://github.com/icholy/xagent/commit/5db29d59eda509f7dea562580c31e6e8d5e61b93))


### Miscellaneous

* move runner-sse-events proposal to accepted ([e5a2f7c](https://github.com/icholy/xagent/commit/e5a2f7c852dec139b29eb69f9dddabc0c8de406a))
* **notifyserver:** exercise bearer and cookie auth paths for /events ([186880f](https://github.com/icholy/xagent/commit/186880f4ca6c7303e9b15b84d676d0d202d375b7))
* **server:** move store-backed auth adapters into server package ([#685](https://github.com/icholy/xagent/issues/685)) ([6466efc](https://github.com/icholy/xagent/commit/6466efcaa45cd35a8cf12fbac74c17ff02e16a5f)), closes [#683](https://github.com/icholy/xagent/issues/683)

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
