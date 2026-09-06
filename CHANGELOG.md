# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Conventional Commits](https://www.conventionalcommits.org/).

## [0.11.0] - 2026-09-06

### ⚠️ Breaking Changes

- **(v0.11.0)** Promote dev ([81ba2c6](https://github.com/orkestra-cc/orkestra/commit/81ba2c622e9dad337c8b7edd80e20a2f780bbc6d))
- **(release)** Bump version to 0.11.0 ([66e9b1f](https://github.com/orkestra-cc/orkestra/commit/66e9b1f085b38f5424a0bd90563bb9703af2c80f))
- **(docker)** Credentials never default — derive the RustFS root from STORAGE_* ([97b1983](https://github.com/orkestra-cc/orkestra/commit/97b19833f21e087797ea46a161f6dffca79359f6))

### Features

- **(sdk)** Surface index-spec drift as an error, not routine noise ([26bf39b](https://github.com/orkestra-cc/orkestra/commit/26bf39b79689f07ee8d014eb0a61e6ae52a11367))
- **(spa)** Handle reauthentication_required on both consoles ([78ae9e2](https://github.com/orkestra-cc/orkestra/commit/78ae9e21a905ceeba02f88d0dec84624afcdfaa2))
- **(auth)** Add the auth_time and mfae claims ([8c61338](https://github.com/orkestra-cc/orkestra/commit/8c61338c6646b184d0ba56bfd49255d7b7d4f070))
- **(user)** Add the MFA epoch field and its additive SDK seam ([f35f3a4](https://github.com/orkestra-cc/orkestra/commit/f35f3a4d10ae7d87fd6fed10e4983b46b5c3dad9))
- **(auth)** Add a bounded dispatcher for transactional auth mail ([2c8bc69](https://github.com/orkestra-cc/orkestra/commit/2c8bc6998981b2a8f202b64a79859f2c0412ab7d))
- **(auth)** Give the IP lockout scope its own admin-managed pair ([3862e62](https://github.com/orkestra-cc/orkestra/commit/3862e62cff1bda255e52903add3350be9aa87123))
- **(auth)** Add the Redis attempt counter and its atomic script ([ed092e8](https://github.com/orkestra-cc/orkestra/commit/ed092e83ec5ccc3bd8eea433c7ed26cee112d0d7))
- **(metrics)** Add auth attempt-counter, lockout and mail-drop families ([243bf38](https://github.com/orkestra-cc/orkestra/commit/243bf38c9375eb0e50bb48545605a0e1cb6fa8e7))
- **(auth)** Add the auth.too_many_attempts wire shape with Retry-After ([7f1c716](https://github.com/orkestra-cc/orkestra/commit/7f1c716d18c7af6ef9c776c4cd0e9efcec4fe8fc))
- **(scripts)** Refuse placeholder or short secrets before a staging/production deploy ([ea24766](https://github.com/orkestra-cc/orkestra/commit/ea247668e2dba595f2f8a20121d65609edf821b0))
- **(client)** AccountDsrPage speaks through i18n ([33e0f9f](https://github.com/orkestra-cc/orkestra/commit/33e0f9f8eb70be4884076077b0d7f8bbbc467c5d))
- **(client)** Proactive rotation 30 s before expiry, outside the 401 comparison; the layout waits for the bootstrap ([88d3717](https://github.com/orkestra-cc/orkestra/commit/88d3717ccba42193e41c20b7a5827338d2e0ea5b))
- **(client)** Add authedFetch — one authenticated path, one 401 algorithm ([0cc22fa](https://github.com/orkestra-cc/orkestra/commit/0cc22fa5008e2e71dd67fbbb20a02bba84cd0400))
- **(client)** Add refreshAfterUnauthorized, the un-gated authenticated retry ([9df62ff](https://github.com/orkestra-cc/orkestra/commit/9df62ffe931a8e2462629b459a0fa6237f0aec89))
- **(client)** Record the access token's expiry from the reported duration ([a7ad2b4](https://github.com/orkestra-cc/orkestra/commit/a7ad2b40077b648facf882aa583f2528092f4a93))
- **(client)** Add jwtExp, the signature-free expiry fallback ([af55347](https://github.com/orkestra-cc/orkestra/commit/af5534715b044809f7b3062ac1a68bf638a11146))
- **(auth)** RequireAuth answers an expired bearer with code access_token_expired ([63ef078](https://github.com/orkestra-cc/orkestra/commit/63ef0784603235eaf8e9f85613a39c6e1faf69a3))
- **(sdk)** Add iface.ErrUserNotFound so auth can classify a deleted account ([3cb3672](https://github.com/orkestra-cc/orkestra/commit/3cb3672c755a039f6c57912b38b878ae322d3365))
- **(frontend-client)** Hide password sign-up, recovery and the header CTA when the method is off ([63ccf0e](https://github.com/orkestra-cc/orkestra/commit/63ccf0ec2886871ef4f86f754b894697539154e7))
- **(frontend-client)** /auth/callback page — scrub before the first request, cookie bootstrap, MFA in memory ([77fefae](https://github.com/orkestra-cc/orkestra/commit/77fefaed2f76457ee7aade453a0de1772d049dc9))
- **(frontend-client)** SSO-capable login page — policy gate, provider buttons, no-method alert ([a75a384](https://github.com/orkestra-cc/orkestra/commit/a75a384fc3bec0d4687eec1746c677b310ffe467))
- **(frontend-client)** Unconditional refresh + bootstrapFromRefreshCookie for a relay-set cookie ([6da5865](https://github.com/orkestra-cc/orkestra/commit/6da586500b08321f27c3a3a2be78bc8cbb68dfef))
- **(frontend-client)** Policy fields, providers list and OAuth start in the auth API ([bb1639b](https://github.com/orkestra-cc/orkestra/commit/bb1639bcccf04200054448a6298f977c57f0b4f5))
- **(frontend-client)** OAuth security primitives — safe next, return-target record, closed callback parser ([eda8c61](https://github.com/orkestra-cc/orkestra/commit/eda8c61c98b673c3e9cfe3b749048ddee0d8af83))
- **(frontend-admin)** Security pages read the split password fields; reset button honours the per-surface policy ([1e77665](https://github.com/orkestra-cc/orkestra/commit/1e77665a30ac51de9b323672e4c30811ac958b59))
- **(frontend-admin)** Hide password UI per policy, labelled break-glass form, no-method alert ([2f37e0f](https://github.com/orkestra-cc/orkestra/commit/2f37e0fd633fc97c40c08861cf97aad1e3ea5639))
- **(auth)** /policy exposes persisted password-login state and the operator break-glass display flag ([08318cd](https://github.com/orkestra-cc/orkestra/commit/08318cd92e73b5a9eb36a70625640cfb9f79bcd0))
- **(auth)** Unlink guard counts usable links only; auth-methods view splits password presence from usability ([6aeff60](https://github.com/orkestra-cc/orkestra/commit/6aeff60dc1d313025a3be6577aa9d8d20a9600f4))
- **(auth)** Step-up honours the per-surface password policy; password-confirm refuses a disabled method ([774e7a4](https://github.com/orkestra-cc/orkestra/commit/774e7a44be6bf543f4601938c9de06022f5fefa2))
- **(auth)** Re-check password policy at MFA/WebAuthn completion; challenges carry audience and break-glass provenance ([ec95640](https://github.com/orkestra-cc/orkestra/commit/ec956408da764f2a01076fb380c801bb10075c7d))
- **(auth)** Per-surface password-method gates on login, register, forgot-password and both admin reset routes ([4abd86c](https://github.com/orkestra-cc/orkestra/commit/4abd86c0a8386007aced3f48570c928ca47bff5b))
- **(auth)** Password-login schema pair, hot-reload declaration and snapshot validator with anti-lockout invariant ([c6b3d50](https://github.com/orkestra-cc/orkestra/commit/c6b3d50464f02fea484e43719f0c810d4b5d1ab3))
- **(auth)** Strict per-surface password-login policy read with operator-only break-glass ([d2a24aa](https://github.com/orkestra-cc/orkestra/commit/d2a24aaf2a8e752a8422b8cabbb5a322cc9a1ec2))
- **(frontend-admin)** OAuth callback page — closed parser, scrub and take in the first passive effect, awaited session, local MFA panel, 10-minute return target ([ff4f9ff](https://github.com/orkestra-cc/orkestra/commit/ff4f9ff2c72f8595ec58c9081eb9519b591cfcc4))
- **(auth)** Provider list and OAuth start resolve usability strictly from one config read — 503 document-level, 403 per-provider ([b585898](https://github.com/orkestra-cc/orkestra/commit/b585898717d20f5ac93b19786d2915265f35d61f))
- **(auth)** Closed OAuth callback contract — one per-tier builder file, allowlisted codes, MFA in the fragment, relay destination ([b1097e1](https://github.com/orkestra-cc/orkestra/commit/b1097e18f6bc0cd0ae07d7d6ee38494dd0e1c0a9))
- **(auth)** Strict one-read OAuth provider usability (ProviderStructurallyConfigured, OAuthWebProviderUsable, UsableWebProviders) ([ecf5989](https://github.com/orkestra-cc/orkestra/commit/ecf5989d08d66394cc5cacd18f582ba76067c02b))
- **(auth)** Strict OAuthAutoLinkByEmailEnabled, ErrAuthPolicyUnavailable / ErrOAuthEmailUnverified sentinels and their error codes ([53f0a03](https://github.com/orkestra-cc/orkestra/commit/53f0a032939feea82b0aee2ef6a202321f6836be))
- **(sdk)** ActiveConfigRequiredModule — one consistent, decrypt-checked read of a required module's active profile ([f4babf9](https://github.com/orkestra-cc/orkestra/commit/f4babf9058b82e8e1aa62971b515b6b1b74d7119))
- **(admin)** Latch module.config_revision_stale with Reload & review, never auto-retry ([957e3b7](https://github.com/orkestra-cc/orkestra/commit/957e3b735eab0fe8df72ba2aeea1e94a9cd869c5))
- **(sdk)** Audit every module-config mutation; config CAS before lifecycle side effect ([6678c23](https://github.com/orkestra-cc/orkestra/commit/6678c23a4088537a9061f188b7f226e2e10834e5))
- **(sdk)** SeedFromModules backfills schema keys with a non-empty fallback ([a2ed98a](https://github.com/orkestra-cc/orkestra/commit/a2ed98a8f46fcbd101a7b3bb4a976d3657e4b7fc))
- **(sdk)** RequirePersistedConfig — auth document fails closed, list shows a missing row ([d27836b](https://github.com/orkestra-cc/orkestra/commit/d27836bfbf9568c1066a3b92dd9da08f0fb3d9a6))
- **(sdk)** Module-config mutations are one CAS write validated on the target snapshot ([bd39620](https://github.com/orkestra-cc/orkestra/commit/bd396201ae0f4026f36282900c0a34612b11f0c4))
- **(sdk)** HasConfigSnapshotValidator + target-profile validation snapshot ([f0921fd](https://github.com/orkestra-cc/orkestra/commit/f0921fd0a267d401e2535577094e04f774217379))
- **(sdk)** ConfigRevision + single-UpdateOne CompareAndSwapConfig on module_configs ([2057f13](https://github.com/orkestra-cc/orkestra/commit/2057f134b4fa443672e81bcbbe0e901801a947c0))

### Bug fixes

- **(frontend-admin)** Let inline code take its context's step, not a fixed one ([4e197c1](https://github.com/orkestra-cc/orkestra/commit/4e197c1d751cff748b47cfdc7e7bc950013cb50a))
- **(frontend-admin)** Stop the operator console overflowing at narrow widths ([2ed08c3](https://github.com/orkestra-cc/orkestra/commit/2ed08c32777f980e26102e449ce77cedd3c51a49))
- **(ci)** Rifiuta un golangci-lint diverso da quello pinnato, invece di fidarti di quello che trovi ([de1704d](https://github.com/orkestra-cc/orkestra/commit/de1704d3df84e7909072fc09d7367f67423266a7))
- **(mobile)** Rigenera pubspec.lock con l'SDK pinnato, cosi' mobile-lockcheck torna verde ([86b144c](https://github.com/orkestra-cc/orkestra/commit/86b144c6c75d218764edc1308e0c3ea5200d3952))
- **(frontend-admin)** Apply the date-column convention to the last two tables ([7b5e58a](https://github.com/orkestra-cc/orkestra/commit/7b5e58a52e442b284b64e68fb4709b8d81365470))
- **(backup)** A failed Redis copy must fail the component, not ride on a sibling ([e3f723c](https://github.com/orkestra-cc/orkestra/commit/e3f723caa0648a24b05bb716ace1a86e44f20488))
- **(backup)** Pin the aws-cli image instead of floating on :latest ([4e7b5c8](https://github.com/orkestra-cc/orkestra/commit/4e7b5c85f915531f5729f38d516963bf8a3ca70c))
- **(backup)** Refuse to write a partial backup, and schedule it verifiably ([0c9086d](https://github.com/orkestra-cc/orkestra/commit/0c9086d3858de734a22e62b5aafc5228ac569ee2))
- **(backup)** Run the rustfs s3 sync as the invoking user ([b3b2fcf](https://github.com/orkestra-cc/orkestra/commit/b3b2fcfde1a92e694dcc1678c4194c8b162f71b7))
- **(frontend-admin)** Make compliance date columns searchable by what they show ([53c7d7e](https://github.com/orkestra-cc/orkestra/commit/53c7d7e53955ac51421c42158ef2e07773cc7359))
- **(frontend-admin)** Make security date columns searchable by what they show ([5c44f36](https://github.com/orkestra-cc/orkestra/commit/5c44f36eb1eeeaf52a36ad22601ed8bf08cde5da))
- **(frontend-admin)** A failed security summary query is not a zero ([9771997](https://github.com/orkestra-cc/orkestra/commit/9771997d1b6c12120fbb092fa78b0479fe4c366a))
- **(frontend-admin)** Bring /user/security onto the console design system ([f5bbba9](https://github.com/orkestra-cc/orkestra/commit/f5bbba9b34b39b42f3d3a0bd37b357ed030c4d2e))
- **(ci)** Stop backend-vulncheck reporting a dead scanner as a clean scan ([3697122](https://github.com/orkestra-cc/orkestra/commit/36971225ba1ba8dc39b955e2c36c3059f32ede0d))
- **(auth)** Name the verdict 401s and cap /mfa/enroll/confirm ([4cfa253](https://github.com/orkestra-cc/orkestra/commit/4cfa2531b41a6f68367f63037721e01fdb866963))
- **(spa)** Point the MFA banner's Set up button at the enrolment page ([41b57b4](https://github.com/orkestra-cc/orkestra/commit/41b57b44a77ee332c25acd231786bdbb10ab0ab5))
- **(auth)** Pin the self path's partial-failure rule; record the owed D16 amendment ([6086c60](https://github.com/orkestra-cc/orkestra/commit/6086c60c2a5bfa8a6a4dbf85b223e41ea89d85a4))
- **(auth)** Stop a factor removal from locking an obliged user out for good ([2e73be7](https://github.com/orkestra-cc/orkestra/commit/2e73be7c91cc6c8e6c6e16f01210b71610631cce))
- **(auth)** Stop a challenge-store outage spending the passkey attempt cap ([ebed86f](https://github.com/orkestra-cc/orkestra/commit/ebed86f1a8c8e1ba79e70600b9332695a60f92d1))
- **(auth)** Refuse password-confirm for MFA-obligated users; cap MFA verify ([84fadfc](https://github.com/orkestra-cc/orkestra/commit/84fadfc16db05cc63d270ef052f3e86e45fa5097))
- **(spa)** One return path for every interceptor redirect, honest comments ([9d9bd2e](https://github.com/orkestra-cc/orkestra/commit/9d9bd2e408d30f83088ef4b15ca4f3f00d978813))
- **(auth)** Stop the step-up mints laundering a removed factor's marker ([2532ebe](https://github.com/orkestra-cc/orkestra/commit/2532ebe715b051d832603066bffa90815cc74274))
- **(auth)** Enforce the MFA epoch and recompute markers on refresh ([7301cd1](https://github.com/orkestra-cc/orkestra/commit/7301cd199bd99b605d27c3b9e21ae1ec389c8bd5))
- **(auth)** Apply a credential change's consequences at destruction, not at success ([424aebe](https://github.com/orkestra-cc/orkestra/commit/424aebea3021e3100344420ac30269eb048def1a))
- **(auth)** End what a removed factor authorised, everywhere, at once ([ba6ef09](https://github.com/orkestra-cc/orkestra/commit/ba6ef09560bfdba60cae42b743d2575ae9dbfe61))
- **(auth)** Fold RemoveFactor's device-trust fake into the existing one, close review gaps ([dc965bf](https://github.com/orkestra-cc/orkestra/commit/dc965bf4eec2b3ffc6d004e7c5751cbfb00fa7fb))
- **(auth)** Make RemoveFactor remove every factor, not just TOTP ([3cbe981](https://github.com/orkestra-cc/orkestra/commit/3cbe9817289674797d95a54fb7d7451e24d1f76d))
- **(auth)** Check the token proof before the lookup; narrow the SDK seam ([528dac3](https://github.com/orkestra-cc/orkestra/commit/528dac320c644acfb9c4e9af9a55cd847b60c657))
- **(auth)** Demand a fresh proof for every MFA enrolment (H-2, H-3) ([49837cd](https://github.com/orkestra-cc/orkestra/commit/49837cd6a1ea4b97ba65e58d373e8c794ac3ecb4))
- **(auth)** Address task-2 review — step-up mint test, mfae on the tenant-scoped mint ([92578f5](https://github.com/orkestra-cc/orkestra/commit/92578f5597f07f0440b64f0e089404fc75776fe9))
- **(user)** Address task-1 review — tenantscope, real Mongo coverage, dead tests, doc tense ([67f31b6](https://github.com/orkestra-cc/orkestra/commit/67f31b6b480117ba88842cb5af61c05bf36302d8))
- **(blob)** Keep x-amz-checksum-mode out of presigned GET signatures ([d69f4cd](https://github.com/orkestra-cc/orkestra/commit/d69f4cd7c87e2c4bac8e75fb05e0099def80c7da))
- **(docker)** Forward TRUSTED_PROXY_* in the dev compose too ([3222863](https://github.com/orkestra-cc/orkestra/commit/32228639792741027530c7dfe6cc8257d2520db6))
- **(authz)** Let only a platform administrator disable a system role ([31ffab8](https://github.com/orkestra-cc/orkestra/commit/31ffab89c05bcd0417dc54ca6259f26fd0f85a3b))
- **(user)** Audit the target-lookup refusal; name the operator routes correctly ([639d80d](https://github.com/orkestra-cc/orkestra/commit/639d80d6fb52f153122b5e402089c6ac69b2fd09))
- **(authz)** Close the empty-scope flush, the disable inversion and the create rewrite ([c8c0392](https://github.com/orkestra-cc/orkestra/commit/c8c039253d6e1debba619b068d76c46e6690adf8))
- **(user)** Let the synthetic dev token resolve a caller role again ([d76e694](https://github.com/orkestra-cc/orkestra/commit/d76e6947a13195409cf15c9ff6fecd735a399475))
- **(user)** Guard the client-tier role paths the way the operator ones are ([5f65f6d](https://github.com/orkestra-cc/orkestra/commit/5f65f6d64424714db99d311cdb585d45f7b86ac1))
- **(user)** Refuse a role change whose pre-read failed, not write it blind ([0351fc4](https://github.com/orkestra-cc/orkestra/commit/0351fc4054e4105ad09c781eac321d121b0d7b8c))
- **(user)** Read the caller's role from the database in the tier guards ([ed44299](https://github.com/orkestra-cc/orkestra/commit/ed4429989c89278f0302642823d67c6bd33fef9b))
- **(user)** Make a system-role change take effect on the next decision ([5330755](https://github.com/orkestra-cc/orkestra/commit/5330755dbbfefccc92d9b6f0575420025d4e98b8))
- **(authz)** Route a role edit that removes a permission past the gate ([2e9a2dc](https://github.com/orkestra-cc/orkestra/commit/2e9a2dcb38ef2eecc7dbdb0a7953741b9aefdc44))
- **(authz)** Stop refusing revocations when the cache cannot be retired ([1db8ae9](https://github.com/orkestra-cc/orkestra/commit/1db8ae982b5db021364d982406b06a159b2e5a87))
- **(authz)** Stop a mid-flight read republishing a retired verdict ([906ba68](https://github.com/orkestra-cc/orkestra/commit/906ba6808cb21b81190e9993b4bcc84971e51b56))
- **(authz)** Gate every permission mutation on a cache it can retire ([e06a7bb](https://github.com/orkestra-cc/orkestra/commit/e06a7bbb1884155c5cfc498a373118d95122d900))
- **(authz)** Make the permission cache generation-keyed ([04b3f15](https://github.com/orkestra-cc/orkestra/commit/04b3f1505edd565c73377a72600de620ac19e754))
- **(authz)** Stop stamping tenant roles on global authorization checks ([9331881](https://github.com/orkestra-cc/orkestra/commit/93318810cce67de71d19aae82075155e0c45da78))
- **(authz)** Correct the D22 comments and restore a hollowed-out fixture ([7ca50eb](https://github.com/orkestra-cc/orkestra/commit/7ca50eb4d1c05d20389ea641770ec554e4c0645c))
- **(authz)** Forbid system.* actions without a platform role (H-5) ([8a35993](https://github.com/orkestra-cc/orkestra/commit/8a359932b22dbc16a10e40d2c7fe7e1c2f0548d5))
- **(authz)** Enforce rule 4 — tenant bindings grant no platform keys ([c2af078](https://github.com/orkestra-cc/orkestra/commit/c2af078eddd97bc6bcd7bdee4d70f5d1736144e1))
- **(authz)** Validate custom-role permissions like bindings (H-4) ([6e38c11](https://github.com/orkestra-cc/orkestra/commit/6e38c11ed09351185ee5ceb947db5809acab9e46))
- **(docker)** Let redis keep the capabilities its entrypoint needs to drop privileges ([83441f7](https://github.com/orkestra-cc/orkestra/commit/83441f7a1fcb84c8fe05bbf5d534cbf07952b87e))
- **(ci,docker)** Pin mongo to its patch and close the CI redis half-pin ([2e57116](https://github.com/orkestra-cc/orkestra/commit/2e571165a8def2ce964427237558b271151527d5))
- **(docker)** Make staging's health check notice a stale backend binary ([b8a8dd1](https://github.com/orkestra-cc/orkestra/commit/b8a8dd19bfe03b8d2a85add78eb2bcf044c1f013))
- **(docker)** Warn when the RustFS S3 API binds to loopback behind a public endpoint ([b9c4aeb](https://github.com/orkestra-cc/orkestra/commit/b9c4aeb6bad5de2e0db8b4216001345291d545e3))
- **(docker)** Digest-pin redis instead of :latest ([1752ef3](https://github.com/orkestra-cc/orkestra/commit/1752ef3d1b08ece32ce69827f3d914883af6d84f))
- **(docker)** Pin staging's Node images alongside the Go one ([08a2e59](https://github.com/orkestra-cc/orkestra/commit/08a2e5930c270cd08a7b250017afb16376d0e51d))
- **(docker)** Pin staging's Go image instead of the floating :1 tag ([4891eeb](https://github.com/orkestra-cc/orkestra/commit/4891eebd19b7e78f3325b0773ba4ad942a16ddd3))
- **(docker)** Make the compose gates hermetic against a local docker/.env ([a81e6e0](https://github.com/orkestra-cc/orkestra/commit/a81e6e0dbdaca1c883701cc0f380b83fa784efbe))
- **(auth)** OR the cumulative durable cap back into recordVerifyFailure ([c97511a](https://github.com/orkestra-cc/orkestra/commit/c97511a85eafe135e39416f687b87cd931cfeec2))
- **(auth)** Give the H-1 race probe a shared bucket to actually contend ([4eb0247](https://github.com/orkestra-cc/orkestra/commit/4eb024763d29269c4f662ac040f491d486c09a79))
- **(auth)** Remove the anonymous-request process crash (H-1) ([85d9d0c](https://github.com/orkestra-cc/orkestra/commit/85d9d0c7079635f71aa4a61473ef24770eee5015))
- **(auth)** Drop the self-matching grep recipe from the D7 comment ([2404fa0](https://github.com/orkestra-cc/orkestra/commit/2404fa0a3e69a67af77a8449f7667999571317da))
- **(auth)** Correct Task 10's comments and cover the actual lockout arm ([30e61f3](https://github.com/orkestra-cc/orkestra/commit/30e61f33ece0114bc78fb653facdfeb87cf26bcb))
- **(auth)** Move the service-account grant onto the attempt counters ([cc1fbd2](https://github.com/orkestra-cc/orkestra/commit/cc1fbd29ea5f941b6ae121910e824e67beb6df77))
- **(auth)** Extract the lockout gate Login already had right, close the gap it left in the copies ([6306b67](https://github.com/orkestra-cc/orkestra/commit/6306b67af3d2b5b35b7d8767bf44afec94e3ad93))
- **(auth)** Put change-password and password-confirm behind the lockout ([0330281](https://github.com/orkestra-cc/orkestra/commit/0330281c9f92eaefa596cdaa6d5c1affe8e3640d))
- **(auth)** Pin M-6 against the store ResendVerification actually wrote to ([e69e8f9](https://github.com/orkestra-cc/orkestra/commit/e69e8f9a1589ac21b4e1023857b772ad6919e605))
- **(auth)** Give reset and resend their own request caps and a detached send ([6227ea8](https://github.com/orkestra-cc/orkestra/commit/6227ea8a4bdaa37135aadccc0d04be9a25d5ac8f))
- **(auth)** Move login lockout onto the Redis counters, closing the oracles ([24c992e](https://github.com/orkestra-cc/orkestra/commit/24c992e2cfd50f6403e0a9320259dccaef37f0bc))
- **(auth)** Close three ways the mail dispatcher could lose or crash mail ([cce7c52](https://github.com/orkestra-cc/orkestra/commit/cce7c52961bd5745c3771bf69c7959499d3d2fa9))
- **(auth)** Correct the stale Incr/Expire rationale left by the script move ([b71d435](https://github.com/orkestra-cc/orkestra/commit/b71d4357f2b3c5377299366919731e6b942cac42))
- **(auth)** Make the MFA per-challenge counter atomic with its TTL ([24dd462](https://github.com/orkestra-cc/orkestra/commit/24dd4625f841136229922029b59194d6fe0e92b0))
- **(auth)** Compare the values the lockout-threshold rule will actually enforce ([6777957](https://github.com/orkestra-cc/orkestra/commit/6777957d3b2696608de490634edcc3971625c380))
- **(orkestra.sh)** Capture the clone-version pin once, keep re-resolution idempotent ([25d2164](https://github.com/orkestra-cc/orkestra/commit/25d2164a6ab3e320f701148e6c2d8bdc1404f338))
- **(orkestra.sh)** Honor a caller-exported ORKESTRA_CLONE_VERSION ([b483e5b](https://github.com/orkestra-cc/orkestra/commit/b483e5be41e4c8aaf08a955629a19366ae40d0db))
- **(server)** Gate /docs + /openapi.json behind API_DOCS_ENABLED, pin Scalar and tighten its CSP ([5529672](https://github.com/orkestra-cc/orkestra/commit/5529672b1b343481fbb89647050ee4c24531308a))
- **(scripts)** A deployment with object storage disabled still needs a RustFS root ([c934a56](https://github.com/orkestra-cc/orkestra/commit/c934a567a432323f7ef7c6da5a5bfd6fd3c1f74d))
- **(config)** Refuse a placeholder object-storage secret in production-like environments ([54abff8](https://github.com/orkestra-cc/orkestra/commit/54abff8aafdb9e4d9aef99d240756d25a8b27397))
- **(docker)** Persist redis data inside its volume (--dir /data) ([ac25fd8](https://github.com/orkestra-cc/orkestra/commit/ac25fd8f5eb098c1d1bc418a6ff4c199cf41a64a))
- **(user)** ResetMFAGrace and the MFA-grace stamp translate the repo not-found ([2a4a095](https://github.com/orkestra-cc/orkestra/commit/2a4a0956bf2913c39bf68bf0a89843ca62a1075b))
- **(cli)** Env-validate compares sites, not hosts — real domains and a disabled client tier pass; localhost stays whole-host ([7bccace](https://github.com/orkestra-cc/orkestra/commit/7bccace349148e85c8a9cd6fe3984135490c1794))
- **(cli,docker)** Env-validate refuses a cross-site client or operator layout, and deploy runs it ([9098a4c](https://github.com/orkestra-cc/orkestra/commit/9098a4c9398b1a1ac71c50b93cf81f6fee445fb2))
- **(frontend-admin)** The refresh classifier is an allowlist — only a 401 ends the session; an unreadable or tokenless 2xx retries ([cf1adad](https://github.com/orkestra-cc/orkestra/commit/cf1adadc77baa29be93f4f3187ac820ecd197f2b))
- **(frontend-admin)** Refresh without replay on a proof-less 401; AbortController timeout with a body race; 2-arg lock pinned; dead OAuth callback removed ([76805be](https://github.com/orkestra-cc/orkestra/commit/76805be0e4816d8a5ebae381ce6314c3b7968b20))
- **(auth,user)** Localhost redirect validator accepts /v1/auth; user-service delegations translate the repo not-found; golden-table scope stated ([ede96d5](https://github.com/orkestra-cc/orkestra/commit/ede96d56ae47724bacc191ba80ddce5134c745a2))
- **(config)** OAuth redirect defaults point at the mounted /v1/auth routes ([2233b36](https://github.com/orkestra-cc/orkestra/commit/2233b364ebbeeb4724f79b0a962d277fe6a0d5b7))
- **(auth)** Keys-not-loaded is a 503 on the refresh path and in RequireAuth; service-account lookups classify outage vs not-found ([2edd5e3](https://github.com/orkestra-cc/orkestra/commit/2edd5e34c86fd26367b403edcadf983783f84a57))
- **(client)** The bootstrap wait says so, and its comment stops claiming a flash ([3e5b364](https://github.com/orkestra-cc/orkestra/commit/3e5b36435655bcb7c12dcc13a02d4d8b1eb7fb1f))
- **(cli)** The setup wizard stops prescribing the cross-site client layout ([12f320f](https://github.com/orkestra-cc/orkestra/commit/12f320f5929f61a9199980e3195bca18d2055f2a))
- **(client)** The route guard waits for the session bootstrap and login honours next ([d30624e](https://github.com/orkestra-cc/orkestra/commit/d30624e9661bf0d25322c2ef3db74b57c0798a88))
- **(docker,docs,config)** The operator console is safe only while its origin and VITE_API_URL agree; client-host defaults and comments say client.localhost ([98b6aa6](https://github.com/orkestra-cc/orkestra/commit/98b6aa6131de725c40e4eb2e5e3aa073efda04dc))
- **(docker,docs)** The client SPA and its API are same-site in dev — client.localhost for both ([62a0e6b](https://github.com/orkestra-cc/orkestra/commit/62a0e6b8fbfa382e1aad109853f778704e10a998))
- **(frontend-admin)** Passkey login routes join the auth allowlist; the reactive gate's soundness claim is scoped to protected routes ([f9b63be](https://github.com/orkestra-cc/orkestra/commit/f9b63be7e6db0842a72a4708cd5af3e29279556f))
- **(frontend-admin)** Reactive refresh only on proof the handler never ran (no more change-password replay) ([7b3e86c](https://github.com/orkestra-cc/orkestra/commit/7b3e86cdd60bbf911f878d7d3305b85ddcce0438))
- **(auth)** The session bootstrap mint answers 503 refresh_lookup_unavailable, not a codeless 401 ([f05a5be](https://github.com/orkestra-cc/orkestra/commit/f05a5bede17b3f75f01634e85f17ef1d02a00764))
- **(auth,user)** Errors.Is for the not-found sentinel, and a sweep that can fail ([8d6a457](https://github.com/orkestra-cc/orkestra/commit/8d6a45779c2d6dad0d4c88ec63eb2e8ad4c64562))
- **(client)** A rejecting lock request is unavailable, never a rejection ([283c604](https://github.com/orkestra-cc/orkestra/commit/283c6049052e81c828c610b9abcbfea22ad23158))
- **(client)** Serialise, bound and correctly classify the refresh (defect B) ([f9fd3ab](https://github.com/orkestra-cc/orkestra/commit/f9fd3abf3c12d6c883773975afd76beb0884b3a2))
- **(auth)** An unreadable family state during a rotation race answers 503, never a revocation ([5e91534](https://github.com/orkestra-cc/orkestra/commit/5e91534f5a92980e8754b559a3a947eb10e66f85))
- **(auth)** Answer 503 refresh_lookup_unavailable when the refresh path cannot complete ([2f89ba2](https://github.com/orkestra-cc/orkestra/commit/2f89ba29169bbac412243b45ca9c8a7b7d1f5524))
- **(docker)** Pass STORAGE_* credentials to the production backend ([3ecc871](https://github.com/orkestra-cc/orkestra/commit/3ecc8713cb74a3f12456655fb74510a7f28bf1e4))
- **(frontend-client)** SanitizeNext re-asserts the single-slash prefix on the parsed pathname ([267e0e7](https://github.com/orkestra-cc/orkestra/commit/267e0e7a97a5e29b71b220bc5b2237da10b051ab))
- **(frontend-admin)** Stop flagging stored required secrets as missing (#324) ([4ee5573](https://github.com/orkestra-cc/orkestra/commit/4ee55736a589f3c0f9760b58813b87627143f2b8))
- **(auth)** Forgot-password handler propagates only the policy sentinels; truthful docs on the strict-policy seams ([5bd4117](https://github.com/orkestra-cc/orkestra/commit/5bd4117d335f3897cc7c0247e86a03afe2eaaefa))
- **(frontend-admin)** Reset button blocks only a known-off method; usability-aware only-credential copy; hide the SSO hint when the method is off ([6ccba97](https://github.com/orkestra-cc/orkestra/commit/6ccba97928308b3bb23667cb477b2917df7cf00e))
- **(frontend-admin)** Hide the social divider when no password form renders; anchor login gating tests on settled state ([02b0449](https://github.com/orkestra-cc/orkestra/commit/02b0449b0f48ff715dc1d6a95baaab9fdbf72d14))
- **(docker)** Raise mongodb nofile ulimits to prevent EMFILE crash-loop (#322) ([b1b64b9](https://github.com/orkestra-cc/orkestra/commit/b1b64b95c7cb09f22dc83ce4d9e03ac5e147971a))
- **(auth)** Stamp the mode pair on relay records; correct the client-tier bootstrap docs; drop orphaned debug helpers and TokenResponse ([74a5357](https://github.com/orkestra-cc/orkestra/commit/74a535787615ff33c3b714b7edaf7c846918b3f5))
- **(auth)** One trust-before-destination OAuth callback flow — client-tier logins complete through a one-shot relay on the client API host; GitHub sets the refresh cookie; dead Huma Apple callback removed ([aad7875](https://github.com/orkestra-cc/orkestra/commit/aad787574d2c3343e000453c975f5d4dcd5814f4))
- **(auth)** OAuth state is one-shot (atomic Take) and bound to the endpoint's provider; cross-host browser binding is deferred to a relay record, never skipped ([e77ce7d](https://github.com/orkestra-cc/orkestra/commit/e77ce7d54f9c2c4f0a865584f74d9e88337c7a3b))
- **(auth)** Accept Apple's string-or-boolean email_verified; report a failing GitHub /user/emails as a provider error, not an unverified address ([21e45c8](https://github.com/orkestra-cc/orkestra/commit/21e45c8c4c92091c4d925285160a7515a864aadc))
- **(auth)** Require a provider-verified email and an establishable auto-link policy before any local email lookup; GitHub email from /user/emails only ([2a92189](https://github.com/orkestra-cc/orkestra/commit/2a92189a0859311dc5dc63d6dc4af88c838464ad))
- **(sdk)** Activation mutations must supply a mirror equal to the profile; repair plaintext-only legacy mirrors at boot ([89313a1](https://github.com/orkestra-cc/orkestra/commit/89313a1f11ec8e370e1184f7f49fff865b95d47c))
- **(sdk,admin)** Strip plaintext on backfill/migration/activation; CAS the needsRestart clear; fail a failed stop; schema-keyed drafts; awaited module refresh ([004eeb9](https://github.com/orkestra-cc/orkestra/commit/004eeb9bc0b74546be181bf053104db832d4e07a))
- **(admin)** Clear the reload draft on baseline identity; test nit ([533c928](https://github.com/orkestra-cc/orkestra/commit/533c9283bdd24a4c32e4d9c222896a854d1dd0fc))
- **(sdk,admin)** Strip schema secrets from every non-secret map; keep needsRestart on combined PATCH; refuse orphan element keys; audit the targeted env; reload refreshes the module ([0213b10](https://github.com/orkestra-cc/orkestra/commit/0213b108ade56560f06b9168723d7c35d58258c8))
- **(sdk,admin)** Final-review wave — own deadline for the boot gate, recover covers emitAudit, lane check before migration, missing KPI ([a6fca94](https://github.com/orkestra-cc/orkestra/commit/a6fca94253962e9417e48b0e25aefef6931c592f))
- **(admin)** Keep the stale-revision banner on screen while Save is latched ([b9b1003](https://github.com/orkestra-cc/orkestra/commit/b9b1003641ac881f9257c48a82a54c6693672d83))
- **(sdk)** ConfigMutation rejects a nil map instead of normalizing it into a wipe ([78194d4](https://github.com/orkestra-cc/orkestra/commit/78194d4b77be7d4b287df3bdc644c64ca2879820))
- **(ops)** Point the production smoke test at /health ([a86b8da](https://github.com/orkestra-cc/orkestra/commit/a86b8da287b5a6cbfb48ac8751f9a768c197a898))

### Style

- **(frontend-admin)** Format the CLAUDE.md paragraph this branch edited ([4834306](https://github.com/orkestra-cc/orkestra/commit/4834306ae1c996d4a4dc43987e896089e6433c71))

### Performance

- **(frontend-admin)** Stat-poll the dev-server watcher only under WSL ([ca24e61](https://github.com/orkestra-cc/orkestra/commit/ca24e61429b8981782a43310640710b230ed1b61))

### Refactor

- **(authz)** Thread an actor through CreateRole and UpdateRole ([f103e9a](https://github.com/orkestra-cc/orkestra/commit/f103e9aa99933d1dc10062aff776b0ee3aae3ee1))
- **(auth)** One coded-error writer behind the middleware emitters; not-found classified by sentinel, not by message ([9ec00f6](https://github.com/orkestra-cc/orkestra/commit/9ec00f65f7efb464b4b7ed7b97a7ed97161c1efb))
- **(client)** One local clear, one error shape, and comments that are true ([9ccd94c](https://github.com/orkestra-cc/orkestra/commit/9ccd94cbf240a98996c71ab111a93e5a04f90003))
- **(client)** Route every authenticated call through authedFetch and delete the dead one ([8fcf522](https://github.com/orkestra-cc/orkestra/commit/8fcf522102945e469e179ddfd5a20ba67eb2227c))

### Documentation

- **(repo)** Record the normalization commit in blame-ignore ([7809910](https://github.com/orkestra-cc/orkestra/commit/78099100d29c57a94fe2a4d6ef4624de515ec02a))
- **(spec)** V1.15 — D27 splits by direction, D36 guards the system-role toggle ([048cc62](https://github.com/orkestra-cc/orkestra/commit/048cc62be9e1cbb74fc17d138038da7d09f4a6cb))
- **(auth)** Close the PR B doc sweep — epoch, gate and step-up corrections ([42ac686](https://github.com/orkestra-cc/orkestra/commit/42ac6869187705f16e2aa5ad5b59721cef626e61))
- **(auth)** Stop calling RequireEnrolmentProof a RoleMiddleware method ([4741a3c](https://github.com/orkestra-cc/orkestra/commit/4741a3c1055ca4afab99b167aa8e91135cad2763))
- Retire every sentence the role cascade and the cache rewrite falsified ([af66da2](https://github.com/orkestra-cc/orkestra/commit/af66da2dc4a9c6e805a9693476ca9d38e46df5bc))
- **(user)** Correct two comments the client role guards left untrue ([4316a7d](https://github.com/orkestra-cc/orkestra/commit/4316a7df401c512c74b2fc3acc32ef944a1d8047))
- **(docker)** Record Redis's CVE floor and why a pin is not a posture ([8ffa53c](https://github.com/orkestra-cc/orkestra/commit/8ffa53c152672d7b6ff3c96e3c5b8a3982d2e95c))
- **(spec)** State M-7's residual in D3, where the closure claim is made ([05e6701](https://github.com/orkestra-cc/orkestra/commit/05e670121f9fae9cb23866aa677fcad6b490ce5c))
- **(spec)** Amend D4 to the cumulative cap PR A actually shipped ([44eff47](https://github.com/orkestra-cc/orkestra/commit/44eff479907eee85cf06e6142b75867d9bf5b986))
- Correct the Redis version to what the stack actually ships ([d8ec379](https://github.com/orkestra-cc/orkestra/commit/d8ec3799db922962dcc7a7df735b12838073b67e))
- **(auth)** Document the Redis attempt counters and the lockout contract ([e9e3a36](https://github.com/orkestra-cc/orkestra/commit/e9e3a36fe5dd83609a77a3faac63011f0aba8f6f))
- **(auth)** Correct the RateLimiter dead-write claim — the ip: key is read ([c5ca59b](https://github.com/orkestra-cc/orkestra/commit/c5ca59b15f8d0837def9f61592ff6cd053720d93))
- **(auth)** Correct the dummyVerify branch claims and record M-7's residual window ([1d9e67e](https://github.com/orkestra-cc/orkestra/commit/1d9e67e266da8a97717004088717f715aedd62a8))
- **(superpowers)** Add the auth/authz audit remediation spec and its five plans ([c0ccc12](https://github.com/orkestra-cc/orkestra/commit/c0ccc12eac93eb061f9d7b49afaa9a152ceb91a5))
- **(docker)** Document credential generation, RustFS root derivation and rotation ([785e3f2](https://github.com/orkestra-cc/orkestra/commit/785e3f2bedea9269ad42f5a549cbb14f7b656996))
- **(auth)** OAUTH_*_REDIRECT_URL seeds the admin key while it is absent — stored values win, env is not inert ([3a91a97](https://github.com/orkestra-cc/orkestra/commit/3a91a97ed6fee22b26df71a2802479cb7a87354c))
- The IdP gets the auth module config, not the compiled redirect fallback ([d3a9e4b](https://github.com/orkestra-cc/orkestra/commit/d3a9e4b5c9c0522d5a0fe9352c3f10d1e516b9b2))
- **(cli,docker)** The guard compares sites; wizard re-runs rebuild CLIENT_API_URL from the host; harness pins the abort reason ([1dd91cb](https://github.com/orkestra-cc/orkestra/commit/1dd91cb6c61038174c55a5f550c27f10aba09747))
- **(dev)** The operator tier is localhost end to end; cookie name samples match the code ([aec0830](https://github.com/orkestra-cc/orkestra/commit/aec08301adce18593b0c4f752f9ccdc96568496b))
- **(spec)** The last §5.8 label points at §5 item 7 ([d11619d](https://github.com/orkestra-cc/orkestra/commit/d11619dafd11e47ba2246ef3e412665abbd0f9b4))
- **(spec)** V22 fixes — boundary-test totals, one emitter claim, #4/#13 scopes, eight clarifications ([6f3b934](https://github.com/orkestra-cc/orkestra/commit/6f3b934fa8929f4ad3210639c9329e236ace79eb))
- **(spec)** Client 401 recovery v22 — proactive rotation §4.11, console refresh-without-replay, keys-not-loaded 503, batch-3 follow-ups ([1a5ece6](https://github.com/orkestra-cc/orkestra/commit/1a5ece6d8b85b2ec133ce8964fecdeab116e3f8a))
- The cookie-hardening page stops arguing with itself; citations re-derived at HEAD ([6466690](https://github.com/orkestra-cc/orkestra/commit/64666902fa96422bb67957f7792b9e4f02b76807))
- **(spec)** §8 #1 and #10 stop contradicting §5/§7/#13; follow-ups 14-16 named ([6e5f0e5](https://github.com/orkestra-cc/orkestra/commit/6e5f0e55265c676c465aa659d549629575783a00))
- **(auth)** The PATCH boundary refuses an out-of-range TTL; a malformed DB value is the other route to the env fallback ([51ae27c](https://github.com/orkestra-cc/orkestra/commit/51ae27c4c85c74dec5e54e75d232fe4837a9af11))
- **(auth,docker)** The access-token TTL is the admin key; the env var is not reachable in practice ([eae4a6d](https://github.com/orkestra-cc/orkestra/commit/eae4a6d38b829294487406f5587dfdd85f13505a))
- **(auth,client)** Six refresh responses, the retry's terminal 401, and 4b's test title ([ccb1cd8](https://github.com/orkestra-cc/orkestra/commit/ccb1cd82071635f04eb04357918d303ede65cb39))
- **(spec)** Client 401 recovery v20 — mint sites, §6 residue, 4b, clock-ahead, follow-ups 5/9-11 ([8d4d392](https://github.com/orkestra-cc/orkestra/commit/8d4d3923bd5f0453c15ff3f3dee44e2a0c62cc9f))
- **(auth)** A terminal-code 401 ends the client session without any refresh call ([81bb4a2](https://github.com/orkestra-cc/orkestra/commit/81bb4a23cdfc2b35259f1f6077c8b861eecb1a99))
- **(sdk,auth)** State the UserProvider not-found obligation, drop three false counts ([6a490c0](https://github.com/orkestra-cc/orkestra/commit/6a490c04bd426cb28a8bb90116a4bdd2c2f9194c))
- **(client)** Three refresh entry points; README stops describing the deleted client ([78876dc](https://github.com/orkestra-cc/orkestra/commit/78876dcaa86ed668df78192a9366d0a3288c454e))
- **(client)** BootstrapFromRefreshCookie's comment states the §4.1 allowlist ([aff3b28](https://github.com/orkestra-cc/orkestra/commit/aff3b28a5f65929f1202ba83f8aaeefe31156972))
- **(auth)** State the expired-vs-invalid 401 split where RequireAuth is documented ([2b9397d](https://github.com/orkestra-cc/orkestra/commit/2b9397dd5daa143a97dd70b3863777c67e014129))
- **(auth)** Scope the refresh 503 claim to the rotation path ([3a6ec63](https://github.com/orkestra-cc/orkestra/commit/3a6ec6364f86e2eac9bd775954afa52307f54828))
- **(plan)** Client 401 recovery — spec v19 + implementation plan (#325) ([acb0346](https://github.com/orkestra-cc/orkestra/commit/acb034607148d1fc5c582be9955b846ede7b03d4))
- **(spec)** Client 401 recovery — rounds 11-14, N4 withdrawn ([a448b9f](https://github.com/orkestra-cc/orkestra/commit/a448b9f8beb0ba999290cacd7cb0122cbd9e7e0d))
- **(spec)** Client-tier 401 recovery design (#325) ([a12806e](https://github.com/orkestra-cc/orkestra/commit/a12806e30386c6cbc56a5a50bf90d9b650cc6952))
- **(deploy)** Spell out that COMPOSE_PROJECT_NAME follows APP_NAME on forks ([a5cf211](https://github.com/orkestra-cc/orkestra/commit/a5cf211566a7ece208e3908c0a08196b8bbc10cb))
- **(frontend-client)** Describe the surface the base actually ships ([82f2525](https://github.com/orkestra-cc/orkestra/commit/82f252528a4cbbba39f347a43eb069d7d5042076))
- **(frontend-client)** Correct three comments the final review flagged ([9c31168](https://github.com/orkestra-cc/orkestra/commit/9c311687341bd52b260e65d81d25982c819dc70b))
- **(frontend-client)** Current surface includes web OAuth login; README layout tree matches the tree ([1c4c8ce](https://github.com/orkestra-cc/orkestra/commit/1c4c8cea7665296133416b50601d598bfd740b36))
- **(plan)** PR 4 client OAuth login — v4 after review round 3 ([e57a974](https://github.com/orkestra-cc/orkestra/commit/e57a97421d4e38196b246bdaf321fd6c37571617))
- **(plan)** PR 4 client OAuth login — v3 after review round 2 ([fc809d4](https://github.com/orkestra-cc/orkestra/commit/fc809d4d8669d9e6cf267e6fb493fca18e4fa6e2))
- **(plan)** PR 4 client OAuth login — v2 after review round 1 ([d5802a7](https://github.com/orkestra-cc/orkestra/commit/d5802a7c3e3f27bd95094690964a0383af7f98c2))
- **(plan)** PR 4 — client OAuth login implementation plan (v1) ([371aa1e](https://github.com/orkestra-cc/orkestra/commit/371aa1eda40d66ab016e508088ed2b60e37d5637))
- **(auth)** Scope the client-tier UI claim to PR 4; verdict-table pointers, path and rule fixes; kept-password notice copy ([cbe8c13](https://github.com/orkestra-cc/orkestra/commit/cbe8c13e8aa83cb75356f86011fc430f054b28b6))
- **(auth)** SSO-only surface, break-glass procedure, split auth-methods fields and the iface sentinels ([3e01973](https://github.com/orkestra-cc/orkestra/commit/3e01973aeb7cb3e4df2aa8d39c87d8b989a81976))
- **(auth)** Step-up docs state the service-audience 503 truthfully; main.go wiring comment updated ([b45cb4d](https://github.com/orkestra-cc/orkestra/commit/b45cb4dfbbb66928a5681d8846bd3b1fcf16a83e))
- **(plan)** PR 3 round 5 — policy stub created before the tests that import it, honest User/BackendUser fixtures with seeded auth state, ripple steps pinned to verified enumerations ([a2f8be0](https://github.com/orkestra-cc/orkestra/commit/a2f8be0f995959f64c20177d57b97f192b6ceadc))
- **(plan)** PR 3 round 4 — verbatim response copy, per-caller unlink preludes, complete frontend test files with settled-state anchors, shared policy stub, runStepUpThrough shown; spec status approved ([e866ece](https://github.com/orkestra-cc/orkestra/commit/e866ece27dc5876cc50718a390c3e6390ba04ea7))
- **(plan)** PR 3 round 3 — deviations resolved against spec v4.5, real User import and locale keys, tests matched to component reality, last placeholders expanded ([c44c7f8](https://github.com/orkestra-cc/orkestra/commit/c44c7f8d8131da33da2a2e84693c6cf002e1d073))
- **(spec)** Password-login toggle v4.5 — iface sentinel homes, up-front malformed-boolean rejection, nil-policy Register outage (PR 3 plan decisions) ([ee95efb](https://github.com/orkestra-cc/orkestra/commit/ee95efb7706c46bc99a026ad38d9fa50d6a0e165))
- **(plan)** PR 3 round 2 — per-task same-commit docs, full test code for every touched surface, deviation decision table, PR 1 dispatch citations, session trailers ([43e51fa](https://github.com/orkestra-cc/orkestra/commit/43e51fa74ce44f973796b8e1ae3b1b840e5b1c80))
- **(plan)** Add the PR 3 password-login toggle implementation plan (10 tasks, spec v4.4) ([5937393](https://github.com/orkestra-cc/orkestra/commit/59373934d3ef41b9f4a64c58c4059ef1e00364b4))
- **(spec)** OAuth callback page takes the return target in the first passive effect, not a layout effect (react-router drops navigate() from a mount layout effect) ([a5bcef7](https://github.com/orkestra-cc/orkestra/commit/a5bcef74cc568699ef816d82b205ae82cd9955cd))
- **(auth)** OAuth web flow — one-shot state, provider binding, client-tier relay, per-tier SPA, closed callback contract, strict provider usability ([234e536](https://github.com/orkestra-cc/orkestra/commit/234e5364280b5edd9e8eb0e4aafb346f16ea28e9))
- **(plan)** PR 2 — name the two relay exceptions in the plan, add the failed-relay-store test, drop the obsolete Origin comment, Architecture v4.4 ([19e02d1](https://github.com/orkestra-cc/orkestra/commit/19e02d1b6dc5c72b7a0b41a8d5ab014181e5c67e))
- **(spec)** PR 2 plan — name the two relay exceptions, test a failed relay store, drop the obsolete Origin comment, editorial fixes ([aea7747](https://github.com/orkestra-cc/orkestra/commit/aea774785c1085be01e4ad6a805c1010f2952102))
- **(spec)** Password-login toggle v4.4 — binding order, relayed client-tier failures, headers on every callback response, closed SPA parser; update PR 2 plan ([0c3e81e](https://github.com/orkestra-cc/orkestra/commit/0c3e81e7b3a553e06aec5c019548ad62c56344ee))
- **(spec)** Password-login toggle v4.3 — client-tier OAuth relay, one-shot state, provider binding; rewrite PR 2 plan tasks 5-10 ([2237b5b](https://github.com/orkestra-cc/orkestra/commit/2237b5bcbf26b98cf67d31a5212b904d6ef6e3cb))
- **(spec)** Password-login toggle v4.2 — PR 2 decisions and findings; add the PR 2 OAuth callback hygiene plan ([e4348c6](https://github.com/orkestra-cc/orkestra/commit/e4348c6934c91e83b25c261f764c08d62e8cb8c3))
- **(plan)** Strip trailing whitespace ([f41b138](https://github.com/orkestra-cc/orkestra/commit/f41b138cfd13dca9c80d493902558fee36315e3e))
- **(sdk)** Document request-lane validation and GetConfig migration propagation ([f222444](https://github.com/orkestra-cc/orkestra/commit/f222444851e90c904b0474b2c202280e2c024099))
- **(plan)** PR 1 plan — review round 3 ([0ec615f](https://github.com/orkestra-cc/orkestra/commit/0ec615f51126f2bf0d45d50afcf9346a2da4b77a))
- **(spec)** Password-login toggle v4.1 — contract decisions from PR 1 planning ([a99e5ed](https://github.com/orkestra-cc/orkestra/commit/a99e5ed510d70a3898a3b8ac952b273405a7d410))
- **(plan)** Password-login toggle PR 1 — SDK config integrity ([a69ffda](https://github.com/orkestra-cc/orkestra/commit/a69ffda3d671c909377a54590ea6146bef8bbaac))
- **(spec)** Password-login toggle v4 — code-check corrections + four-PR split ([110b0e3](https://github.com/orkestra-cc/orkestra/commit/110b0e3887bad67f99525f87de209ffe87594eb1))
- **(spec)** Harden password-login toggle design v3 ([717c91a](https://github.com/orkestra-cc/orkestra/commit/717c91aa13845a777977608eccb15fc68c852551))
- **(spec)** Password-login toggle v2 — answer the review ([9891148](https://github.com/orkestra-cc/orkestra/commit/989114860970f1765e00355a545d52d17d0bfeaa))
- **(spec)** Per-surface password-login toggle — design ([37fc366](https://github.com/orkestra-cc/orkestra/commit/37fc366d1395e917f782db6591b602abc757fd71))

### Tests

- **(frontend-admin)** Cover every date column, and close two vacuous guards ([f083dab](https://github.com/orkestra-cc/orkestra/commit/f083dab44f4c034ab3d379d0874f0fd72de94a6f))
- **(frontend-admin)** Anchor the sessions-failure test on the error copy ([8a68c4f](https://github.com/orkestra-cc/orkestra/commit/8a68c4fd41a06e14daf22c72d24fa20b09ce012d))
- **(authz)** Assert Cedar reasons by membership, not by Reasons[0] ([2c1982e](https://github.com/orkestra-cc/orkestra/commit/2c1982e9f2c66d4ec08dd4bba1a82a66c690c7cb))
- **(authz)** Cover the role-write handler layer; cut echoed keys on a rune boundary ([2a95f43](https://github.com/orkestra-cc/orkestra/commit/2a95f43ecabd5467af4294466fdfad31a1df2843))
- **(client)** The G3 case pins both refresh hits, not just the outcome ([ac1733e](https://github.com/orkestra-cc/orkestra/commit/ac1733e61571fa4a5d1d195e20be03e2c20b8b2f))
- **(client)** Assertions that can actually fail (null payload, timer, marker) ([6ad5278](https://github.com/orkestra-cc/orkestra/commit/6ad527841d629c541122a3f18c09ac16e202caf1))
- **(client)** Pin credentials, retry body and no X-Retry on authedFetch ([ed7faf8](https://github.com/orkestra-cc/orkestra/commit/ed7faf8a245fb11cdf32c93fb6a52ab76e61f34e))
- **(service-accounts)** Hold the PATCH open instead of a 50 ms delay in the double-click tests (#326) ([ee21e70](https://github.com/orkestra-cc/orkestra/commit/ee21e707ed6483245d7ab203418324b9f81870ae))
- **(frontend-client)** Restore vi.spyOn spies in the shared afterEach ([1692616](https://github.com/orkestra-cc/orkestra/commit/1692616166e606caad4fdff05fcfcd666029f317))
- **(frontend-client)** Add Vitest + RTL + MSW harness and the client-test CI gate ([fa14718](https://github.com/orkestra-cc/orkestra/commit/fa14718082309f18568ca531e951051ef8b4fbc6))
- **(auth)** Correct two comments to what the new cases actually pin ([da7d034](https://github.com/orkestra-cc/orkestra/commit/da7d0348ea32f1bbb402c76cb5209207df0a628a))
- **(auth)** Pin open-route verdicts, validator binding, unknown-audience override and nil-policy completion ([53949c3](https://github.com/orkestra-cc/orkestra/commit/53949c3789967abde91a8dd27b47704a8d6314f3))
- **(sdk)** Pin that a complete legacy document is not rewritten at boot ([9d5592a](https://github.com/orkestra-cc/orkestra/commit/9d5592aab4f5916ad8bbbd255e949cd2042b9601))

### Build

- **(deps-dev)** Bump happy-dom in /frontend-admin ([d6a7e6d](https://github.com/orkestra-cc/orkestra/commit/d6a7e6dff5edf96d17c8c84b24f16aa860419756))
- **(deps)** Bump uuid from 14.0.1 to 14.0.2 in /frontend-admin ([6036c84](https://github.com/orkestra-cc/orkestra/commit/6036c846a72afaa30f0d90fce76e4f5106006eb8))
- **(deps)** Bump web-vitals from 5.3.0 to 6.2.1 in /frontend-admin ([6ab1e10](https://github.com/orkestra-cc/orkestra/commit/6ab1e10e86a0cb77d9f276cb0cbe89172b267dbd))
- **(deps)** Bump github.com/go-webauthn/webauthn in /backend ([0feaf29](https://github.com/orkestra-cc/orkestra/commit/0feaf2990f4fdf80a6dc00b4417c364148149a2a))
- **(go)** Go 1.26.8, x/crypto 0.56.0, AIR v1.67.4 ([869b470](https://github.com/orkestra-cc/orkestra/commit/869b470f4a7113f581a7351a62c75a87385d44f3))
- **(make)** Scripts/init.sh joins the backend CI surface ([3d35774](https://github.com/orkestra-cc/orkestra/commit/3d357744db00a1ed43958062a87d590ef95269e1))

### CI

- **(backend)** Run the script tests and the credential gate on env/script changes ([287e183](https://github.com/orkestra-cc/orkestra/commit/287e183b724cb868b4aeb706a33fab29b2a26521))
- **(backend)** Set MONGO_TEST_URI so guarded Mongo integration tests run in CI (#321) ([af82641](https://github.com/orkestra-cc/orkestra/commit/af82641ad1b14d7d14d3b14c10278f38414ddbd5))

### Dependencies

- **(deps)** Move the otel and aws-sdk-go-v2 families as families ([e4becb2](https://github.com/orkestra-cc/orkestra/commit/e4becb212142c91df75110f6a4fdb722e0319768))
- **(deps)** Bump github.com/docker/go-connections in /backend ([9e44b0a](https://github.com/orkestra-cc/orkestra/commit/9e44b0a858c628fee6441f852023353f4002540d))
- **(deps)** Redis 7.4 redis-stack-server -> 8.2.9 plain redis ([801a04c](https://github.com/orkestra-cc/orkestra/commit/801a04ce2c5a625aaa2dd483894b52f4e5135d61))
- **(deps)** Upgrade MongoDB 8.0.23 -> 8.0.29 ([4be5379](https://github.com/orkestra-cc/orkestra/commit/4be53799c8c9b7021558accd1d68bfe531c09c3d))
- **(deps)** Group @fullcalendar/* so Dependabot bumps them in lockstep ([de9143f](https://github.com/orkestra-cc/orkestra/commit/de9143f586cf2ce51fb483f69c178ee6d2518611))
- **(deps)** Bump flutter_secure_storage from 9.2.4 to 11.0.0 in /mobile ([725d75d](https://github.com/orkestra-cc/orkestra/commit/725d75d17e1a280c47e23dd6c3f5747d13a25072))
- **(deps)** Bump actions/setup-java from 5 to 6 ([dd85472](https://github.com/orkestra-cc/orkestra/commit/dd85472e39623367a52b100faf18174b6809d41e))
- **(deps)** Bump actions/checkout from 4 to 7 ([7c25da7](https://github.com/orkestra-cc/orkestra/commit/7c25da7b4fc96a3c3a84c8e844c9321ab199b163))
- **(deps)** Bump react-hook-form in /frontend-admin ([3ca2160](https://github.com/orkestra-cc/orkestra/commit/3ca21602cccc34df19ed838ab400cb060b513038))
- **(deps)** Bump @testing-library/jest-dom in /frontend-admin ([48729cf](https://github.com/orkestra-cc/orkestra/commit/48729cfd07b78fff3689b6ab3f9b30607c0eb3c9))
- **(deps)** Bump dompurify from 3.4.13 to 3.4.14 in /frontend-admin ([4be29eb](https://github.com/orkestra-cc/orkestra/commit/4be29eb18bf5ef1719369c6cc659dc2c47618936))
- **(deps)** Bump github.com/danielgtaylor/huma/v2 in /backend ([a9cb90c](https://github.com/orkestra-cc/orkestra/commit/a9cb90ca73338b00ffa5687f409dcaaf33d0b850))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/credentials in /backend ([bb1c35e](https://github.com/orkestra-cc/orkestra/commit/bb1c35ea2c7a5611a9ea8f64fa42853dd38a63d5))
- **(deps)** Bump github.com/go-chi/chi/v5 in /backend ([3830144](https://github.com/orkestra-cc/orkestra/commit/3830144d1f6e85321a057f8e9fa280ac0334d223))
- **(deps)** Bump github.com/redis/go-redis/v9 in /backend ([8da8fd0](https://github.com/orkestra-cc/orkestra/commit/8da8fd0f600f1a3f704036b1407b122a7fca1be7))

### Chores

- **(sync)** Absorb main after the #369 promotion ([66cddb9](https://github.com/orkestra-cc/orkestra/commit/66cddb9ab46e18b21aa6387a7610f9c315524345))
- **(repo)** Put an end to the drift the hooks recreated on every push ([70989dc](https://github.com/orkestra-cc/orkestra/commit/70989dc1bb8ab246a9efe3f24e037f28f23c016f))
- **(tooling)** Put the chain on one golangci-lint version (2.13.2) ([883f0c9](https://github.com/orkestra-cc/orkestra/commit/883f0c942c8708db9038e1bcd94ecd8507ab79d5))
- **(merge)** Bring dev into feat/auth-authz-audit-remediation ([4bf4b46](https://github.com/orkestra-cc/orkestra/commit/4bf4b4629f54b00edea144582ceccb64d203b9df))
- **(merge)** Bring dev into feat/auth-authz-audit-remediation ([693ff2f](https://github.com/orkestra-cc/orkestra/commit/693ff2fe7a9eb0cdddc0e155d2005b90189192ad))
- **(client)** Drop the dormant openapi-fetch client, its generated types and codegen script ([077ef23](https://github.com/orkestra-cc/orkestra/commit/077ef2315dbc84de071f89ece1fd299b2573577e))

### Merge

- **(main)** Absorb the v0.10.0 promotion back onto dev ([6d75306](https://github.com/orkestra-cc/orkestra/commit/6d75306fcbdd811a1f998985de1a16f9fcc7045a))

## [0.10.0] - 2026-08-29

### ⚠️ Breaking Changes

- **(auth)** Make RequireAuth bearer-only — drop the middleware silent refresh ([1872116](https://github.com/orkestra-cc/orkestra/commit/18721162169e70bda5f35c510d16c3f9b373603f))

### Bug fixes

- **(auth)** Address #317 review findings — structural rotation tripwire + doc fixes ([7769000](https://github.com/orkestra-cc/orkestra/commit/776900043db268f859b11bfd7070f22d123f2597))
- **(frontend-admin)** Bound the proactive refresh fetch with a timeout ([43e432f](https://github.com/orkestra-cc/orkestra/commit/43e432f2f76e76eb56fd4d9ab7e07431c7edf624))
- **(auth)** Enforce a floor on NewJWTService's access-token TTL ([b3fdefe](https://github.com/orkestra-cc/orkestra/commit/b3fdefee3652b160ec3a03c80b3d41931be82ee6))
- **(frontend-admin)** Rotate the access token before it expires (#317) ([30beb96](https://github.com/orkestra-cc/orkestra/commit/30beb9684fa8664729bf69e6ee42cd8da3f9be8a))
- **(docker)** Run bundled MongoDB as a single-node replica set ([1962a0e](https://github.com/orkestra-cc/orkestra/commit/1962a0e41c81f0335f64122445697be1e04247ff))
- **(tenant)** Key the bootstrap queries on the validated org ([2ce8bb2](https://github.com/orkestra-cc/orkestra/commit/2ce8bb2e95aed58bf010eb55a788da836c0a2bd1))

### Refactor

- **(frontend-admin)** Delete the dormant third refresh caller ([25c85dc](https://github.com/orkestra-cc/orkestra/commit/25c85dc138cd9b7fffe8d5b2189cdfed42578174))

### Documentation

- **(auth)** Name both reintroduction guards and their residue; drop a dead guide ([d59812e](https://github.com/orkestra-cc/orkestra/commit/d59812e1c8c68011f170cb26ed3f2d776895113e))
- **(auth)** Correct comments that outlived the middleware silent refresh ([9e8ac9d](https://github.com/orkestra-cc/orkestra/commit/9e8ac9d54470a30294a6683047cf79fe671e6dd3))
- **(adr)** ADR-0020 — RequireAuth is bearer-only, rotation only via explicit refresh endpoints ([9b4cfb6](https://github.com/orkestra-cc/orkestra/commit/9b4cfb619818ad390e4d80413af953f175bac0c1))

### Tests

- **(auth)** Close mint-only gap in the #317 cookie-rotation guard ([f1b00f9](https://github.com/orkestra-cc/orkestra/commit/f1b00f9a36a2fec6991ac715ed5d17743a52bfae))
- **(store)** Guard RTK Query endpoint-name uniqueness ([0dfc576](https://github.com/orkestra-cc/orkestra/commit/0dfc576c1ef6b611157b7a44cbab3b2cbd64e947))

### Release

- **(v0.10.0)** Promote dev ([e68e318](https://github.com/orkestra-cc/orkestra/commit/e68e3180caa5a4b3792a297a28bd1736ce69b758))

## [0.9.0] - 2026-08-26

### Features

- **(notification)** SenderSlug on the delivery log + sender filter (ADR-0019 PR 4) ([7d0b74a](https://github.com/orkestra-cc/orkestra/commit/7d0b74a9224c771156064451aa84d2a1f228b4f8))
- **(notification)** Mailup driver — SendMessage over SMTP+ credentials, allowlisted success, bounded diagnostics (ADR-0019 PR 3) ([3430dcb](https://github.com/orkestra-cc/orkestra/commit/3430dcbaac2edfa05ca850b6c2bdc3e4874c5155))
- **(notification)** Bounded vendor response reader ([23bda45](https://github.com/orkestra-cc/orkestra/commit/23bda45d155e1c99826434f39dc98f3bfa04021d))
- **(frontend-admin)** I18n for the notification sender-profiles group and field (ADR-0019) ([5ecc37f](https://github.com/orkestra-cc/orkestra/commit/5ecc37f733046d9f1aef4bc12629c0d34fdaebf2))
- **(notification)** Explicit sender on the test send with typed error codes (ADR-0019 PR 2) ([81b6c56](https://github.com/orkestra-cc/orkestra/commit/81b6c56bfcbaa4ab4e8ae45cae92dd798933e271))
- **(auth)** Category-aware notification pre-flight on all eight guards; auth.* categories on verify/reset sends (ADR-0019 D7) ([7ba6683](https://github.com/orkestra-cc/orkestra/commit/7ba66833cd46e372faf9bd05728b5e3cc8adb3b2))
- **(sdk)** Optional CategoryConfiguredChecker + IsConfiguredForCategory; notification answers per category (ADR-0019 D7) ([02c18b2](https://github.com/orkestra-cc/orkestra/commit/02c18b2db0e02adae8a5981d8a83d6f04b79933a))
- **(notification)** Declare email.senders, validate the routing map at save and activation, read the roster from the same snapshot as the legacy keys (ADR-0019 PR 2) ([7ec9129](https://github.com/orkestra-cc/orkestra/commit/7ec9129ece1b6ac554d5fafed9b5b33303afa654))
- **(notification)** Save-time sender validation scoped to the three roster states (ADR-0019 D5) ([474f289](https://github.com/orkestra-cc/orkestra/commit/474f289402c13bae8f035844ab369b24f8a1860b))
- **(notification)** Resolve senders by most-specific category pattern; legacy keys carry mail until a profile routes (D6) ([bbf63d4](https://github.com/orkestra-cc/orkestra/commit/bbf63d4d1a4c0b2a208f33daae032533c07cc391))
- **(notification)** Email.senders element schema and roster decoder ([4f4b438](https://github.com/orkestra-cc/orkestra/commit/4f4b43877957192d440d90a14b0344d5df90105a))
- **(notification)** Sender category pattern grammar, normalization and precedence ([227f6ce](https://github.com/orkestra-cc/orkestra/commit/227f6ce4698f9cb034558000adcb000dac80ebbf))
- **(errcode)** Notification.sender_* codes (ADR-0019) ([c25898c](https://github.com/orkestra-cc/orkestra/commit/c25898ccea3335ad5c5b138ce42836311920bc8d))
- **(notification)** Noop driver behind the EmailDriver seam ([fe80299](https://github.com/orkestra-cc/orkestra/commit/fe8029981f99dd1e7fe79f379ff221aa2fa4f285))
- **(notification)** Typed, peer-free send error contract rendered at the chokepoint (ADR-0019) ([782dbc8](https://github.com/orkestra-cc/orkestra/commit/782dbc8accef025d3bdcb7ed4fa028e5917c87c9))
- **(notification)** SenderProfile and the EmailDriver contract (ADR-0019 PR 1) ([7f43ef3](https://github.com/orkestra-cc/orkestra/commit/7f43ef30a6addf1b75fe889aa0b2b43c36311be1))
- **(sdk)** Enforce the record-list label rules and the creation binding ([250a849](https://github.com/orkestra-cc/orkestra/commit/250a849cc9cce3ea4af7115ddbe222dfa23f6576))
- **(admin)** Send record-list membership as explicit intent ([84fe513](https://github.com/orkestra-cc/orkestra/commit/84fe51367aa1c09747e2077f5be03ec8d3b606ed))
- **(admin)** Record-list element cards with staged delete ([a4132df](https://github.com/orkestra-cc/orkestra/commit/a4132df580c8ffefe6c2587a1eb1e468a1481ead))
- **(admin)** Expand record-list schema against the roster ([85a0ae8](https://github.com/orkestra-cc/orkestra/commit/85a0ae8a7720f24af4eb29c5ef043aee2a92246a))
- **(sdk)** Expose record-list mutations on the environment PATCH ([73a9b7d](https://github.com/orkestra-cc/orkestra/commit/73a9b7d99c1c6b65aaaaf7f6460f3234f597374a))
- **(sdk)** Decode a record list into a Go slice ([087baf6](https://github.com/orkestra-cc/orkestra/commit/087baf6e3276dfff3b371ec50979a9b33a406f3e))
- **(sdk)** Record-list mutation with reconcile, validate, CAS and retry ([8f70b5f](https://github.com/orkestra-cc/orkestra/commit/8f70b5faae50914f8e00908571462e93088fd506))
- **(sdk)** Per-environment revision and a sub-document CAS ([bc4cf4e](https://github.com/orkestra-cc/orkestra/commit/bc4cf4e400e0d6c5b9b0f79016902000adde3827))
- **(sdk)** Record-list roster and membership preconditions ([b535b03](https://github.com/orkestra-cc/orkestra/commit/b535b035ca05eda9181428225b78c06fbf8ce7e9))
- **(sdk)** Record-list key composition with a bounded element prefix ([31dceec](https://github.com/orkestra-cc/orkestra/commit/31dceec3ed56a116f8ce7ed6b8eca5401a3c551c))
- **(sdk)** Slug minting and label rules for record-list elements ([8acc3f9](https://github.com/orkestra-cc/orkestra/commit/8acc3f92d540af1d42f1143d6247286dd2dba43d))
- **(sdk)** Validate record-list declarations at boot ([a63d636](https://github.com/orkestra-cc/orkestra/commit/a63d636bebb692968f86a17c10abc1956319ad71))
- **(sdk)** Add the recordList field type and its item schema ([7adef02](https://github.com/orkestra-cc/orkestra/commit/7adef021273cd8d421fb0597ff8f148ec88e1257))
- **(admin)** Platform-default badge, MFA transfer, guarded lifecycle UI ([ad67db9](https://github.com/orkestra-cc/orkestra/commit/ad67db9bb90e8042cdbd242ca67886a0a945aa82))
- **(admin)** Mandatory finalization wizard with resume and recovery states ([72d20ea](https://github.com/orkestra-cc/orkestra/commit/72d20ea67af41e608d7ce2d6ea2348e71d4bab57))
- **(admin)** Phase-aware SetupGate with retryable 503 state ([76fabbc](https://github.com/orkestra-cc/orkestra/commit/76fabbc02eb68985ee7e7b13bdb4aaa1566bc3bc))
- **(admin)** Setup finalization api with ordered session re-mint ([634a075](https://github.com/orkestra-cc/orkestra/commit/634a0757e3ab6e2318bb69f02ffb1d49a0b19db7))
- **(tenant)** Versioned setup reconciliation in Module.Start ([29e965e](https://github.com/orkestra-cc/orkestra/commit/29e965ee97c6c2644b9621fc5032aa2812bc5717))
- **(devtoken)** Resolve the operational platform default ([a07f6b4](https://github.com/orkestra-cc/orkestra/commit/a07f6b4ce535e30a891ebe663d9f36f208c2cf8e))
- **(auth)** Operator tenant fallback prefers the platform default for members ([d617ca6](https://github.com/orkestra-cc/orkestra/commit/d617ca6a40f2e429bb4a26ef6490cf7cb8029747))
- **(setup)** Resumable authenticated finalization saga ([af0f14f](https://github.com/orkestra-cc/orkestra/commit/af0f14fae52d00fb2cbea9c72757128f460c0a3a))
- **(setup)** Read-only finalizer access probe ([dc7060a](https://github.com/orkestra-cc/orkestra/commit/dc7060a542ce10e8183767dcfb4af4ad6e99da9c))
- **(setup)** Split public and authenticated setup route registration ([0344527](https://github.com/orkestra-cc/orkestra/commit/0344527303f1d885c17c1642c4d0e7905a29d259))
- **(setup)** Persistent three-phase status, fail-closed 503 ([93ef87b](https://github.com/orkestra-cc/orkestra/commit/93ef87bb96553fcd30bae39bffad87df197dcd78))
- **(user)** Narrow user lifecycle state provider ([62df0a8](https://github.com/orkestra-cc/orkestra/commit/62df0a848f6190ef2bb572b28785b14c71b3d65a))
- **(tenant)** EnsureSetupTenant reserved-UUID reconciliation seam ([5f01612](https://github.com/orkestra-cc/orkestra/commit/5f016129f67d5e19cbb94d53adde9e7a4812be76))
- **(authz)** Idempotent owner-binding ensure behind unique compound index ([fb387ca](https://github.com/orkestra-cc/orkestra/commit/fb387caf450ffe887182ee563a91a4771c84d25d))
- **(compliance)** Concurrent-idempotent LocalKMS.CreateKey ([c6508ee](https://github.com/orkestra-cc/orkestra/commit/c6508ee7d0309ac3fe6f07787685432ca8076455))
- **(tenant)** Default transfer route, MFA split, derived isDefault ([82abb6f](https://github.com/orkestra-cc/orkestra/commit/82abb6fb9575a2d788398a084457dbac01d43546))
- **(tenant)** Platform default assign/transfer with lifecycle guards ([6d2e1ad](https://github.com/orkestra-cc/orkestra/commit/6d2e1ad0ff99beb607ca5e63b8597d0de1f5e5a2))
- **(tenant)** Tenant_defaults pointer with guarded transactions ([7652593](https://github.com/orkestra-cc/orkestra/commit/765259399bdc6de629007197a27abffdb570b467))
- **(tenant)** Provisioning policy validated on all three module-config surfaces ([83c75cb](https://github.com/orkestra-cc/orkestra/commit/83c75cb5ef07af69c49ecb7b1faf00ce9ceb99a7))
- **(tenant)** Tier-1 provisioning collapses to manual/single, fail-closed ([667221a](https://github.com/orkestra-cc/orkestra/commit/667221ad5e339042a7b80316afc7e69ca4b2a894))
- **(tenant)** CountProvisioningSlotsByKind replaces CountActiveByKind ([d074e4f](https://github.com/orkestra-cc/orkestra/commit/d074e4fb9f965785753b1663f6e40834a35326c7))
- **(server)** Register setup finalization store ([f8c8ceb](https://github.com/orkestra-cc/orkestra/commit/f8c8cebf0d45a28cc6a39e28d85b8e3252e20a54))
- **(systeminit)** Setup_finalization record with CAS/lease FinalizationStore ([792343c](https://github.com/orkestra-cc/orkestra/commit/792343c357680257b8b7297e5740f2445134fe1a))
- **(sdk)** Activation-time config validator hook ([a17745b](https://github.com/orkestra-cc/orkestra/commit/a17745bbcfd956952a76864cd2094b99ae89b856))
- **(sdk)** Optional stable code on config validation errors ([a862f48](https://github.com/orkestra-cc/orkestra/commit/a862f48e6edea705f1e2a0f8b7eac42233b3807b))
- **(frontend-admin)** Let the date layer format in a record's own zone ([c802599](https://github.com/orkestra-cc/orkestra/commit/c8025994101bf1256e350341b18eb991485a9e0e))
- **(navigation)** Add module-owned global nav-action seam ([585c6f6](https://github.com/orkestra-cc/orkestra/commit/585c6f6e4b67471db05408be59b80cc4a2b1ad21))
- **(modules)** Add a module-owned global overlay seam ([e83de10](https://github.com/orkestra-cc/orkestra/commit/e83de103cddc33881dc7ea4af0ed8e55e66ceb7a))
- **(sdk)** Add CRMActivitySink for billing follow-up ([62500d1](https://github.com/orkestra-cc/orkestra/commit/62500d1bf26d70feae749f1b8ac834b81145884e))
- **(ci)** Gate backend error quality from make, with today's backlog frozen ([695ffb0](https://github.com/orkestra-cc/orkestra/commit/695ffb0ab7b7c21c648690be547c154f1550ffcc))
- **(errquality)** Add baseline and allow-comment suppression ([3acbe19](https://github.com/orkestra-cc/orkestra/commit/3acbe19cfc807e2325c6da16a1bb40cd385f3e30))
- **(errquality)** Flag a client status returned from an error mapper's fallback ([4770c9f](https://github.com/orkestra-cc/orkestra/commit/4770c9f3b83274c06037dad7ea96f45c812cf9a5))
- **(errquality)** Flag details that say nothing ([b1703d4](https://github.com/orkestra-cc/orkestra/commit/b1703d4972268a8b23b0432562a2343d4d4c887a))
- **(errquality)** Add the analyzer skeleton and the raw-error rule ([162543c](https://github.com/orkestra-cc/orkestra/commit/162543cac455e310954025bd73b3038bd275d4ab))
- **(errcode)** Add 5xx builders and the two auth availability codes ([75b62ba](https://github.com/orkestra-cc/orkestra/commit/75b62baa967dc99552fc7c118fe638e171c29c53))

### Bug fixes

- **(docker)** Make the production compose actually boot ([f368d12](https://github.com/orkestra-cc/orkestra/commit/f368d1230ebe480e92064cfec82fe44ab811835e))
- **(docker)** Harden production app services and use named volumes ([e97057f](https://github.com/orkestra-cc/orkestra/commit/e97057f253e618e7c2014777a5419a82534d0bc1))
- **(docker)** Bind infra ports to loopback and drop capabilities ([1c43d24](https://github.com/orkestra-cc/orkestra/commit/1c43d24f4329fdd6d190519c1f72b9850a398c33))
- **(admin-modules)** Keep an out-of-range enum value visible and selected ([0a1b4a9](https://github.com/orkestra-cc/orkestra/commit/0a1b4a9b99a28806b746917e3d28fdc9287b624d))
- **(systeminit)** Renew the stage lease by MatchedCount, not ModifiedCount ([c098b9b](https://github.com/orkestra-cc/orkestra/commit/c098b9bb310c4b5077ad2364205b20b2551397e5))
- **(admin)** Exclude record lists from the completeness measures ([5b59630](https://github.com/orkestra-cc/orkestra/commit/5b5963054865f720c835c92fa1c72c68b1436a41))
- **(sdk)** Report secret status for record-list elements ([fc753a8](https://github.com/orkestra-cc/orkestra/commit/fc753a8e595dcfcd7e547580c5c5fef49399a29a))
- **(sdk)** Activate an environment in one atomic write ([8293aae](https://github.com/orkestra-cc/orkestra/commit/8293aae305bad4d65e7cd17e86ed3357b29ae6d0))
- **(frontend-admin)** Forward data-testid through SubtleBadge ([2682d72](https://github.com/orkestra-cc/orkestra/commit/2682d72a9c16f583e378f3a264b86d1b9b246207))
- **(tenant)** Stop reporting startup success on a lost reconcile lease ([27f1f96](https://github.com/orkestra-cc/orkestra/commit/27f1f96b842ac5240ba9cd0a9f8b95487a1b2bbb))
- **(tenant)** Make the single-mode provisioning check atomic ([2cac3c1](https://github.com/orkestra-cc/orkestra/commit/2cac3c170f71fcd22058ddf396b4cb800308c435))
- **(tenant)** Serialize the first default-tenant assignment ([674d7b7](https://github.com/orkestra-cc/orkestra/commit/674d7b768310ff722d58fcfb76c040265f9a82de))
- **(tenant)** Persist why a tenant row was soft-deleted ([9035fc9](https://github.com/orkestra-cc/orkestra/commit/9035fc9bccb513ce8ca2d82b23c026d4ec8025a5))
- **(setup)** Reject a whitespace-only tenant name at finalization ([ed60d88](https://github.com/orkestra-cc/orkestra/commit/ed60d88e5e4d0e937a845d5a362e6d32fc0a49d4))
- **(authz)** Make binding dedup and re-grant expiry-aware ([848854b](https://github.com/orkestra-cc/orkestra/commit/848854b682cb754ac743eaa89f31f1f8ae452c04))
- **(admin)** Correct three untrue tenant-lifecycle statements ([ff446df](https://github.com/orkestra-cc/orkestra/commit/ff446dfa0663d76783025729153a748be61e679f))
- **(tenant)** Gate the delete/purge cascade behind the default guard ([254b556](https://github.com/orkestra-cc/orkestra/commit/254b55643c79817bb1b2a6a49dad2d02312e96e2))
- **(setup)** Correct MFA scope overclaim and wizard role copy ([3707286](https://github.com/orkestra-cc/orkestra/commit/37072862257377f6c6e43abd7224cf53a167a35a))
- **(admin)** Replace literal control bytes with escapes in returnTo's CONTROL_CHARS ([b92b12b](https://github.com/orkestra-cc/orkestra/commit/b92b12b7768bab8525012328a2f24e09ebdb6193))
- **(sdk)** Core-module Start failure aborts startup ([1dff3f5](https://github.com/orkestra-cc/orkestra/commit/1dff3f52da9e7fe223dea4ad156d1671205baaad))
- **(setup)** Restore the finalize Problem Details response, correct the tenant setup contract ([86780fd](https://github.com/orkestra-cc/orkestra/commit/86780fda611a841d9ed002b40c3845a4281c58e2))
- **(authz)** Persist ExpiresAt on EnsureBinding's first insert ([fc814e0](https://github.com/orkestra-cc/orkestra/commit/fc814e067eda7117dfa98649f6b3a97f8a9bba3d))
- **(tenant)** Stamp isDefault on memberDTO, fix admin-transfer op tag ([9ed3ed4](https://github.com/orkestra-cc/orkestra/commit/9ed3ed4214086840ac5d07a8bb0e168befbbeea0))
- **(tenant)** Close AssignDefaultTenant check-then-act race ([2c148ba](https://github.com/orkestra-cc/orkestra/commit/2c148ba4f71abbf5755631c734a187b4fe2d4da3))
- **(tenant)** Give Tier-2 lazy-provisioning lock its own detail; sync docs.orkestra.cc provisioning page ([eb09bd6](https://github.com/orkestra-cc/orkestra/commit/eb09bd634b7b768169b08b3f87389b81edf22a42))
- **(tenant)** Drop forbidden active-tenant shorthand from single-mode gate comment ([54565db](https://github.com/orkestra-cc/orkestra/commit/54565dbcfd044a5e27f799acb8a0833d195b7efa))
- **(frontend-admin)** Derive the switch gutter from the switch track ([ac64033](https://github.com/orkestra-cc/orkestra/commit/ac64033c5f20d740d5be4b53d2f0f8e3e4eeea19))
- **(frontend-admin)** The SOC2 refresh button had no variant to render ([592163b](https://github.com/orkestra-cc/orkestra/commit/592163bc3609fbceecbf66c4031f81d9e9ae74bf))
- **(tooling)** Let conventional-pre-commit accept the chain's merge type ([9759a04](https://github.com/orkestra-cc/orkestra/commit/9759a04ad4b6f090e3084c0de7f8483a515f2659))
- **(core)** Embed tzdata and stop prefix-matching sibling nav leaves ([32bdb33](https://github.com/orkestra-cc/orkestra/commit/32bdb33914d6364f203c42ce6c39759e94d8816c))
- **(tenant)** Preserve duplicate slug conflicts ([73910a2](https://github.com/orkestra-cc/orkestra/commit/73910a2ca5829e1ad01383911da1546e370445be))
- **(authz)** Keep policy internals out of authorization errors ([8873fe4](https://github.com/orkestra-cc/orkestra/commit/8873fe4d80c375d4c3e87e4e3121a35457cbea1f))
- **(tenant)** Keep service failures out of API details ([1b71e05](https://github.com/orkestra-cc/orkestra/commit/1b71e0573405991600f86107bbf24fce9990bb59))
- **(auth)** Report configuration faults as server faults ([ac21992](https://github.com/orkestra-cc/orkestra/commit/ac21992f381ee3cbbbd7d34531a1d42418992ce8))
- **(errquality)** Resolve identifiers R3's default: clause returns ([20d651d](https://github.com/orkestra-cc/orkestra/commit/20d651da08c119f266a1c0cb80d6d52a8553fdba))
- **(errquality)** Narrow R3 to the value the default: clause actually returns ([29c1a41](https://github.com/orkestra-cc/orkestra/commit/29c1a41017432a75cee76ee822d7bbce1b2aeaa0))
- **(errquality)** Normalize whitespace after punctuation trim in R2 ([2b9a57f](https://github.com/orkestra-cc/orkestra/commit/2b9a57f04ac8138a5958b2a2263a811227bc4c76))
- **(frontend-admin)** Serialise refresh rotation across tabs ([5ececd1](https://github.com/orkestra-cc/orkestra/commit/5ececd1c1267f06992b4c2a19fc33053175014be))
- **(auth)** Tolerate concurrent refresh rotation instead of revoking the family ([42a3db6](https://github.com/orkestra-cc/orkestra/commit/42a3db61fd8ded3c27387f32a89958ed7a13646b))
- **(authz)** Raise a Cedar shadow divergence to Error in production ([5349582](https://github.com/orkestra-cc/orkestra/commit/5349582cb6b4732bd0363503dd52b5fbb2c603ed))
- **(admin-ui)** Translate the session-cap field ADR-0017 left untranslated ([8065a9b](https://github.com/orkestra-cc/orkestra/commit/8065a9b936e62788cffdc561303dfe3e468906ca))
- **(frontend-client)** Serve /config.js with no-store from the dev server ([63b53fe](https://github.com/orkestra-cc/orkestra/commit/63b53fe6ebc5a66046675f02f538bbf4bad2bac2))
- **(frontend-admin)** Serve /config.js with no-store from the dev server ([3601c25](https://github.com/orkestra-cc/orkestra/commit/3601c2520e668b2248e2ac4eb39f347a1212bbf3))
- **(server)** Register optional modules in a deterministic order ([e3bbc05](https://github.com/orkestra-cc/orkestra/commit/e3bbc057215c9f817c8fd791a5efebe6881122cc))
- **(modules)** Prevent overview card clipping ([3eed3be](https://github.com/orkestra-cc/orkestra/commit/3eed3be43cfd2b9226f450960aadef0c11673802))
- **(auth)** Preserve super-admin wildcard after refresh ([76b69ff](https://github.com/orkestra-cc/orkestra/commit/76b69ff6b12616cbd56de5b3e1f22ee1a829910b))

### Style

- **(frontend-admin)** Reformat a vendor-chunk list to satisfy eslint --fix ([260097a](https://github.com/orkestra-cc/orkestra/commit/260097a30ca194dfed73dd5d1664a1fe51375c77))

### Reverts

- **(errquality)** Back out R3's identifier resolution ([1981c23](https://github.com/orkestra-cc/orkestra/commit/1981c23a321d4a7b99092dfc76fdb923db7c0257))

### Refactor

- **(notification)** Retire emailService inside the smtp driver; route dispatchEmail through resolver → validate → driver (ADR-0019 PR 1) ([982ed11](https://github.com/orkestra-cc/orkestra/commit/982ed11264f44c14c1e9397f04be4017f52e86e3))
- **(sdk)** Abstract the module config repository behind an interface ([cd7fc61](https://github.com/orkestra-cc/orkestra/commit/cd7fc61de26c92b6b3c3f75c0b2a79ed6f0cba48))
- **(auth)** Rename JWT default-tenant concept to tenant fallback (wire dtid kept) ([481d5ec](https://github.com/orkestra-cc/orkestra/commit/481d5ecba3d39a8ae637865f39fc19aa1a5df67a))
- **(tenant)** Extract absent-to-present creation primitive ([cd5461e](https://github.com/orkestra-cc/orkestra/commit/cd5461ea90c5cb647d127605dc94ccc522ddbc27))
- **(systeminit)** Split first-admin sentinel, inline tenantscope allows ([a995cd7](https://github.com/orkestra-cc/orkestra/commit/a995cd780f7a8bbf62a0bc937e033616cef08554))

### Documentation

- **(site)** Notification sender profiles, routing, drivers and diagnostics (ADR-0019 PR 5) ([3f0b070](https://github.com/orkestra-cc/orkestra/commit/3f0b0705eb41f64c84748561e6a5378e936023dc))
- **(adr-0019)** Record two plan gaps PR 1's execution surfaced ([a1f0768](https://github.com/orkestra-cc/orkestra/commit/a1f0768c6c0edaa7d039bf53f2e30787f5c49c82))
- **(adr-0019)** Add the implementation plan ([280db46](https://github.com/orkestra-cc/orkestra/commit/280db46ee6f71db136bbe945715f2206e4bcea7b))
- **(adr-0019)** Record the amendments the implementation plan surfaced ([98201b8](https://github.com/orkestra-cc/orkestra/commit/98201b8ae1f4bb5ef56b5c9f66b6eeeaec001f23))
- **(adr-0019)** No string from a remote peer is persisted, SMTP included ([ba0b02a](https://github.com/orkestra-cc/orkestra/commit/ba0b02a71cd31c7e2fe7b17a78b6f1074a6dedf5))
- **(adr-0019)** Bound the response read, not just what is stored ([2955bc4](https://github.com/orkestra-cc/orkestra/commit/2955bc464a1b74d791b7a7ef97a6a33b8cc380c2))
- **(adr-0019)** Never persist vendor free text; assert the shape, not the absence ([2e0ba41](https://github.com/orkestra-cc/orkestra/commit/2e0ba411e6482ed151329f4aec25c1bf828d356d))
- **(adr-0019)** Attribute the fix to the pre-flight, not to IsConfigured ([72f157c](https://github.com/orkestra-cc/orkestra/commit/72f157cecf80b4f1a924cb4460cefb03bdb3453b))
- **(adr-0019)** Fix a double em dash left by the guard-count correction ([95d28c2](https://github.com/orkestra-cc/orkestra/commit/95d28c2eb03591b43785471bc5c7db386cb67acd))
- **(adr-0019)** Never persist a vendor response body; bound and sanitize instead ([b4ab514](https://github.com/orkestra-cc/orkestra/commit/b4ab514993611a31b890a754bc700bdf9e7a92fa))
- **(adr-0019)** State the MailUp success predicate instead of an ordering ([0e35ae5](https://github.com/orkestra-cc/orkestra/commit/0e35ae5fd8408969e17ed5e794faeb6e0a040ef7))
- **(adr-0019)** EmailMessage gains Category only, not three fields ([4ee070a](https://github.com/orkestra-cc/orkestra/commit/4ee070ac0863c68422bedaa65d6fa65441656b76))
- **(adr-0019)** Sync the ADR with four spec decisions it had not absorbed ([8ae66b3](https://github.com/orkestra-cc/orkestra/commit/8ae66b3cf04783223269ad0d7c5b3de6764637a8))
- **(adr-0019)** Give every validation check an explicit scope ([249b7a1](https://github.com/orkestra-cc/orkestra/commit/249b7a15befe6b6e3645e7f35b1fc6d1828d6951))
- **(adr-0019)** Retire two sentences the later decisions falsified ([aec9422](https://github.com/orkestra-cc/orkestra/commit/aec942297edc3319e601a1cf258b9edfa325c516))
- **(adr-0019)** Restate precedence as longest-literal; the tie-break was unneeded ([fae43f5](https://github.com/orkestra-cc/orkestra/commit/fae43f50d032399a850b08aa149fe7117d840703))
- **(adr-0019)** Carry category on EmailMessage so CampaignCode is implementable ([64fe631](https://github.com/orkestra-cc/orkestra/commit/64fe63133ce72b7beed8f836d26a08e98edd03b0))
- **(adr-0019)** Scope from_address to the drivers that actually read it ([df66ea7](https://github.com/orkestra-cc/orkestra/commit/df66ea73d7f8d9067b120123ea2b100eabb01bd9))
- **(adr-0019)** The auth guards are eight, not seven ([f7725d1](https://github.com/orkestra-cc/orkestra/commit/f7725d195287d525ae7d047094394577c485213f))
- **(adr-0019)** Pin pattern grammar, move the test-send into PR 2, fix OpenAPI claims ([2b24108](https://github.com/orkestra-cc/orkestra/commit/2b24108bd141e98445e81d9df160cb048b2715d5))
- **(adr-0019)** Correct the MailUp section — endpoint, auth, and webhooks ([b9df8ea](https://github.com/orkestra-cc/orkestra/commit/b9df8ea99b3fecd17b612637b240b03722d64277))
- **(adr-0019)** Pin driver Validate so anonymous SMTP keeps working ([31ad29a](https://github.com/orkestra-cc/orkestra/commit/31ad29a7d6e8a3aaedd71450f3a6d160824a8d16))
- **(adr-0019)** Make pre-flight checks category-aware (D7) ([8c9c4eb](https://github.com/orkestra-cc/orkestra/commit/8c9c4ebbb159a9a4139499563fd2ab4bf8e78091))
- **(adr-0019)** Scope the routing rules to the states where a map exists ([e6e499d](https://github.com/orkestra-cc/orkestra/commit/e6e499d7c5ad5443472ab1d25ce1c38c2cc92186))
- **(adr-0019)** Multi-sender email delivery — profiles, category routing, driver seam ([0589bf4](https://github.com/orkestra-cc/orkestra/commit/0589bf4688bbbba5afb346d9d82d1e4b39e2fbda))
- **(deploy)** Document port exposure, firewall, systemd unit and off-site backups ([1deaa6d](https://github.com/orkestra-cc/orkestra/commit/1deaa6d96438c781e9834e29aca73a693450cf51))
- Correct the stale Vite 7 references for frontend-admin ([c300036](https://github.com/orkestra-cc/orkestra/commit/c3000360eddd41227db9a5de9be8a332da3f9b53))
- **(sdk)** Teach the addon surfaces that repeatable config exists ([fa1b663](https://github.com/orkestra-cc/orkestra/commit/fa1b66315a72e0e52b273da6343b3b9794213d64))
- **(sdk)** Document repeatable config fields ([74ee580](https://github.com/orkestra-cc/orkestra/commit/74ee580f2ad850a53de7b3cec0101931f099c23c))
- **(setup)** Document setup contract, add operator guide, drop dead locale keys ([6c4f1a4](https://github.com/orkestra-cc/orkestra/commit/6c4f1a44998020dd42f98401851df12d4488db6e))
- **(auth)** Reword remaining default-tenant prose to tenant fallback ([ed5aaa7](https://github.com/orkestra-cc/orkestra/commit/ed5aaa7a3a84a39d12eb20c34f33543c14b7ab51))
- **(setup)** Regenerate openapi spec for three-phase status ([2db3bbb](https://github.com/orkestra-cc/orkestra/commit/2db3bbb48a811761c05df1564d8f692ec831940e))
- **(backend)** Update systeminit scope to include setup-finalization saga ([5ec6ed4](https://github.com/orkestra-cc/orkestra/commit/5ec6ed42dab95196c70dc8b4d7cee01af5a39766))
- **(frontend-admin)** The button family is orkestra-*, not falcon-* ([f97f253](https://github.com/orkestra-cc/orkestra/commit/f97f253cbf628fa601ca7b6cfc64e1ece632284e))
- **(plans)** Regenerate the OpenAPI spec in the authz burn-down too ([ce9b290](https://github.com/orkestra-cc/orkestra/commit/ce9b290683f8b9ab426abe7bc81d52b4694ed1f9))
- **(plans)** Plan the errquality gate and the core error burn-down ([76b14ce](https://github.com/orkestra-cc/orkestra/commit/76b14ceb62b02939c0f94680486c3d52a3213518))
- **(specs)** Design an analyzer-first sweep for backend error quality ([11b709d](https://github.com/orkestra-cc/orkestra/commit/11b709d027954501f7476c4e8f689c97e12ecb06))

### Tests

- **(notification)** Pin legacy flat-key compatibility under the driver seam ([c45601e](https://github.com/orkestra-cc/orkestra/commit/c45601e32bc6638cec70f84ae308f64530fd4972))
- **(notification)** Pin today's MIME wire output as a golden before the driver refactor ([45c3cbb](https://github.com/orkestra-cc/orkestra/commit/45c3cbb62c59bdc39d5c23d4b05b22d14564d9c2))
- **(admin)** Pin Retry-After to its header value, not the default ([ca0dfb8](https://github.com/orkestra-cc/orkestra/commit/ca0dfb86444a98f5a0af4153662660115c7411ba))
- **(tenant)** Build the production tenant_defaults unique index in repository tests too ([f752d3e](https://github.com/orkestra-cc/orkestra/commit/f752d3ea80135ab43f4b0e852886e8aadd7053b4))

### Dependencies

- **(deps)** Bump @testing-library/jest-dom in /frontend-admin ([3c17be9](https://github.com/orkestra-cc/orkestra/commit/3c17be9e020fe130aa61ee0d70c9e48b6574b381))
- **(deps)** Bump json_serializable from 6.11.2 to 6.14.1 in /mobile ([f4c1e95](https://github.com/orkestra-cc/orkestra/commit/f4c1e950db60e0aa4e7bd443c4762165cba7af28))
- **(deps)** Bump dorny/paths-filter from 3 to 4 ([06296ed](https://github.com/orkestra-cc/orkestra/commit/06296edf8e4ca23a5a0eeca5a4fc0dd2e1e0f26c))

### Chores

- **(systemd)** Ship the production stack unit the deploy guide describes ([23d3100](https://github.com/orkestra-cc/orkestra/commit/23d3100e8b15b5f8c88dd85a96684d712b05fe94))
- **(deps-dev)** Bump jsdom from 26.1.0 to 30.0.1 in /frontend-admin ([6d8320c](https://github.com/orkestra-cc/orkestra/commit/6d8320c3e5f0d2d6e491cefa5ab660def9f77bb5))
- **(backend)** Regenerate the OpenAPI spec and realign the errquality baseline ([0590a29](https://github.com/orkestra-cc/orkestra/commit/0590a29ccd8cfbd34c53b060d9f871c4fc8dfeed))
- **(frontend-admin)** Re-run prettier on vite.config.js and CLAUDE.md ([62bd6a8](https://github.com/orkestra-cc/orkestra/commit/62bd6a8db2803ac16eed51bcc62c6460b2b570da))
- **(sync)** Absorb the v0.8.0 promotion merge into dev ([090976f](https://github.com/orkestra-cc/orkestra/commit/090976fd1409d2ee24d61185b3c5bd109c5b3e8a))

### Merge

- **(upstream)** Take dev's SDK record-list config work ([ac13809](https://github.com/orkestra-cc/orkestra/commit/ac138093f8728f65a7e5331394c143584ade33ce))
- **(server)** Register optional modules in a deterministic order (#281) ([76bb0a8](https://github.com/orkestra-cc/orkestra/commit/76bb0a8f144499ff7a5746e130ed3eace57449aa))

### Release

- **(v0.9.0)** Promote dev ([49d1a05](https://github.com/orkestra-cc/orkestra/commit/49d1a05e3f93122f908f9d6945be8ddd884bf879))

## [0.8.0] - 2026-08-22

### ⚠️ Breaking Changes

- **(auth)** Record ADR-0017 breaking changes for downstream forks ([ce82d42](https://github.com/orkestra-cc/orkestra/commit/ce82d421ea8088230b69ad98c6983d97b70861dd))

### Features

- **(auth)** Elected self-draining refresh-token retention sweep ([74419ed](https://github.com/orkestra-cc/orkestra/commit/74419ed3e56721576b878e09c086f9542f9e7da8))
- **(auth)** Redis lease electing one maintenance scheduler across replicas ([e254c1f](https://github.com/orkestra-cc/orkestra/commit/e254c1fa3ac802eaad008c9a0c9dc40f29d14ac9))
- **(metrics)** Refresh-token sweep deleted/backlog/duration families ([8b02cde](https://github.com/orkestra-cc/orkestra/commit/8b02cdee70b575124e509ffd843edf52e76f156e))
- **(auth)** Bounded expired-refresh-token sweep with a hasMore probe ([0296893](https://github.com/orkestra-cc/orkestra/commit/02968931943e84bd56ee3590146909d3e9a2eb87))
- **(auth)** TTL index on session documents, retention fallback set to 90d ([7745ca6](https://github.com/orkestra-cc/orkestra/commit/7745ca6268eab0ad34be80b147225fc875dd4700))
- **(auth)** Surface session-cap expiry as its own code and clear the cookie ([9beb140](https://github.com/orkestra-cc/orkestra/commit/9beb140628c3db5328af9b45febd8d7cc8880f9a))
- **(auth)** Enforce an absolute session cap on both refresh paths ([fbaba8a](https://github.com/orkestra-cc/orkestra/commit/fbaba8a9dd0bdb1b8c55f97e0ec8fd350517882d))
- **(metrics)** Session-cap expiry, event-failure and anchor-anomaly counters ([4c9b025](https://github.com/orkestra-cc/orkestra/commit/4c9b0251b08e1136137627ab191dbab2e399507b))
- **(auth)** Idempotent session-expiry transition for the absolute cap ([ae81e91](https://github.com/orkestra-cc/orkestra/commit/ae81e915c1b17d93d373b71429c856cc9afa89cd))
- **(auth)** Declare sessionAbsoluteTTL and its retention-margin invariant ([98c62a8](https://github.com/orkestra-cc/orkestra/commit/98c62a8cd33ee30e1e396ea8c76c7816a60827da))
- **(auth)** Validate credential-governing durations at the config boundary ([e1ff696](https://github.com/orkestra-cc/orkestra/commit/e1ff696bd052a304f6469bac52efb397d3bef360))
- **(sdk)** Optional HasConfigValidator seam on module config updates ([c65c10f](https://github.com/orkestra-cc/orkestra/commit/c65c10ffc798f7abdc473c9f734480297c40bc4f))
- **(blob)** Apply a bucket CORS policy so presigned browser uploads work ([37662ce](https://github.com/orkestra-cc/orkestra/commit/37662cecfc27206fdf06acd728f9031b4c566428))
- **(ci)** Gate lockfile sync and backend coverage from make, not the workflow ([51b75ff](https://github.com/orkestra-cc/orkestra/commit/51b75ff8bfcdd76df8012ed185c91f25a1e8a983))
- **(notification)** Make the default template locale configurable ([4d92a30](https://github.com/orkestra-cc/orkestra/commit/4d92a302ffadf4c32ebfd265f0701c278bf6f8b3))
- **(notification)** Italian translations of the six default templates ([98f2370](https://github.com/orkestra-cc/orkestra/commit/98f2370e0de7fe8c185d6f617fe6913f3c7715c7))
- **(sdk)** Collect and seed module notification templates at boot ([c594c4f](https://github.com/orkestra-cc/orkestra/commit/c594c4f44f2c85edcd27d4cff4ad2de8a1c9f997))
- **(notification)** Seed templates declared by modules ([c62c7b0](https://github.com/orkestra-cc/orkestra/commit/c62c7b0f3f91b36a60c4a5ef1b2e4260287bb58f))
- **(sdk)** Let a module declare its own notification templates ([ff6f333](https://github.com/orkestra-cc/orkestra/commit/ff6f3339d05ede48cf78bbaa8bb1382065d94229))
- **(ui)** Promote the two-metric tile to StatCardPair ([9e3d4d8](https://github.com/orkestra-cc/orkestra/commit/9e3d4d8a484fedd857bb3f216950c02920308f87))
- **(ui)** Add a drill-down footer slot to StatCard ([8305667](https://github.com/orkestra-cc/orkestra/commit/830566732cfda31fb46983c019e0f54983fec9bc))

### Bug fixes

- **(auth,spa)** Make the refresh-error taxonomy reach the user it was written for ([5783f3e](https://github.com/orkestra-cc/orkestra/commit/5783f3e2b2f65d2a295c0a4ff9a491a5072545ca))
- **(auth)** Pin the risk scorer's history window to session retention ([edafb7f](https://github.com/orkestra-cc/orkestra/commit/edafb7f2f39771f3a8007658f00874ed3a6f1632))
- **(sdk,auth)** Stop a failed config read from silently arming the session cap ([5ad35e1](https://github.com/orkestra-cc/orkestra/commit/5ad35e16af4d9570f75b905ece25272aae53cd93))
- **(auth)** Exclude a zero expiresAt from the session TTL indexes ([1fe9491](https://github.com/orkestra-cc/orkestra/commit/1fe9491a994d294df79a97f2ba61a92fe63a6d79))
- **(admin-ui)** Accept the bare-day duration suffix the backend parser takes ([c4d73a1](https://github.com/orkestra-cc/orkestra/commit/c4d73a173525dc618f3469922088be875572e592))
- **(auth)** Renumber the tenantscope baseline after the session-retention change ([0382271](https://github.com/orkestra-cc/orkestra/commit/038227157095d6045e2219e39bde54c08e0d1db3))
- **(auth)** Pin the session-revocation denylist TTL to the policy maximum ([8d7df5f](https://github.com/orkestra-cc/orkestra/commit/8d7df5ff072710d39f11879768a5b3060a9458ac))
- **(auth)** Collapse the access-token 15m default to one source ([bf1815c](https://github.com/orkestra-cc/orkestra/commit/bf1815c40d900b02d2232d58f04bb67d45caada7))
- **(auth)** Restore the admin -> env -> 15m access-token chain ([cb6cb6a](https://github.com/orkestra-cc/orkestra/commit/cb6cb6aea61ee24ef8180ade962e56bd24c989cf))
- **(sdk)** Seed notification templates on RetryInit too ([27a0150](https://github.com/orkestra-cc/orkestra/commit/27a0150b3cd5d07671fd27623024dd3eefe4f7b7))
- **(release)** Commit the refreshed changelog to dev, not main ([1be5db8](https://github.com/orkestra-cc/orkestra/commit/1be5db840546475930dbd818ab13bb977d6aea81))
- **(agents)** Run integrated browser MCP through mise ([6e7eb06](https://github.com/orkestra-cc/orkestra/commit/6e7eb06d1e839fec99ec12c4a28321485e5c3052))
- **(navigation)** Drop duplicate sidebar entry for the navigation workspace ([48e6787](https://github.com/orkestra-cc/orkestra/commit/48e678784bacc2dda495027beb2f8528ad8eae72))
- **(docs)** Unbreak the v0.7.0 docs publish ([92d8e2b](https://github.com/orkestra-cc/orkestra/commit/92d8e2b206666a39d102ed89345af119b941a3bd))

### Refactor

- **(auth)** Single day-aware duration parser for env and admin values ([5419d33](https://github.com/orkestra-cc/orkestra/commit/5419d3348f69182270e9636afded30b9acd08c27))
- **(ui)** Recalibrate the StatCard icon and anchor it to the card ([972be7e](https://github.com/orkestra-cc/orkestra/commit/972be7ee647219cc2a47ceedca1670094068fba8))

### Documentation

- **(auth)** Correct the risk-lookback contract on the session repository ([d931978](https://github.com/orkestra-cc/orkestra/commit/d931978d98f09f298b4b36090969c615a3f7b0fb))
- **(auth)** Link the ADR-0017 fail-closed follow-up to its tracking issue ([c4e2cfc](https://github.com/orkestra-cc/orkestra/commit/c4e2cfc2c7db1bbae43756629bfd68df4e49f3fc))
- **(auth)** Name the real index-conflict error on the session TTL upgrade path ([6a2d5e5](https://github.com/orkestra-cc/orkestra/commit/6a2d5e5a50582f8cdf44330e4ddbb0131d8491db))
- **(auth)** Correct the session retention boundary on the published page ([b7ede66](https://github.com/orkestra-cc/orkestra/commit/b7ede6659097790e3d8a00654b532b051322c04f))
- **(auth)** Document the three session lifetimes and close the last stale-30d mentions ([b425aef](https://github.com/orkestra-cc/orkestra/commit/b425aefe101dc74e996939f75c16bcdbf459cd16))
- **(auth)** Align .env.example to 15m and correct the lifetime comments ([4fb9792](https://github.com/orkestra-cc/orkestra/commit/4fb9792207746fa2e099ec29dfee2c1ec9d25b3b))
- **(adr)** Refine ADR-0017 and add the session-lifetime implementation plan ([c0d57cd](https://github.com/orkestra-cc/orkestra/commit/c0d57cd107f534e757ac5581ae1fee71013339e9))
- **(adr)** Record ADR-0017 — session lifetime, token TTL sourcing, retention ([89f1974](https://github.com/orkestra-cc/orkestra/commit/89f19748d11baf8364bf93df44b0e00596abc5db))
- **(claude)** Wire the module map to the published pages ([2a7e47a](https://github.com/orkestra-cc/orkestra/commit/2a7e47ad39d91200391cafbe123a665843af7714))
- **(site)** Finish the last four Draft pages ([21892e6](https://github.com/orkestra-cc/orkestra/commit/21892e6bf40af439c52987347571763c58a880f6))
- **(site)** Turn the five remaining core-module stubs into real pages ([de5f69a](https://github.com/orkestra-cc/orkestra/commit/de5f69a0e588e80a4d26bf9867ee9941ffd608b9))
- **(site)** Document service accounts and the object-storage seam ([4e9e356](https://github.com/orkestra-cc/orkestra/commit/4e9e356c0eeb1d93d88c1e290b55fe0789b0b0a4))
- Note that Quick Start's localhost assumes HOST_BIND_ADDRESS=0.0.0.0 ([8165e5e](https://github.com/orkestra-cc/orkestra/commit/8165e5e045b56fd2d525a12eb19dccfd693643f9))
- **(sdk)** Use a neutral module name in the template examples ([d164d34](https://github.com/orkestra-cc/orkestra/commit/d164d34527188ff4c7c8f60bffcae941784e7547))
- **(adr)** Unbreak the ADR-0014 docs build ([b751d8c](https://github.com/orkestra-cc/orkestra/commit/b751d8c21b02824b262b7aa944c5eeb07332f9cd))

### Tests

- **(auth)** Pin the lease type assertion and prove the sweep recovers ([dac6368](https://github.com/orkestra-cc/orkestra/commit/dac6368e832c0d544add80269cea0cd84c45ee57))
- **(notification)** Guard that every template exists in every locale ([345d231](https://github.com/orkestra-cc/orkestra/commit/345d23162187ce1c51eedbe1388ab9cff2a31632))

### Dependencies

- **(deps)** Move the whole OpenTelemetry tree to 1.45.0 / 0.21.0 ([062bb89](https://github.com/orkestra-cc/orkestra/commit/062bb89872e75592090a94bfaf5a2502222bc155))

### Chores

- **(auth)** Remove dead COOKIE_MAX_AGE and correct the 30-day refresh claims ([c7c3251](https://github.com/orkestra-cc/orkestra/commit/c7c325133a7230243b454f34ae58065dedee22d4))
- **(sync)** Absorb the #266 promotion merge into dev ([e0bf943](https://github.com/orkestra-cc/orkestra/commit/e0bf943619a509498e221bacbf8f3cfb72a239a1))
- **(sync)** Absorb the #263 promotion merge into dev ([3ed4f6a](https://github.com/orkestra-cc/orkestra/commit/3ed4f6ae039086b14f98582fe6716468e763e4a6))
- **(agents)** Synchronize project MCP configuration ([203ad4c](https://github.com/orkestra-cc/orkestra/commit/203ad4c0f1b46ed737bc47660b02fc4cce7b9c19))

### Release

- **(v0.8.0)** Promote dev ([55e363c](https://github.com/orkestra-cc/orkestra/commit/55e363c69ce9657deb454689937997ea5bb4eb6b))

## [0.7.0] - 2026-08-21

### Release

- **(v0.7.0)** Promote dev ([9f770a7](https://github.com/orkestra-cc/orkestra/commit/9f770a7ac7262510ffa660c751103fc17457e1b4))

## [0.6.0] - 2026-08-18

### Features

- **(observability)** Add logging module workspace ([d16e58d](https://github.com/orkestra-cc/orkestra/commit/d16e58d9c3bde8376596579d0b1b71e24a3c4e52))
- **(observability)** Add logging workspace API ([d5a2be3](https://github.com/orkestra-cc/orkestra/commit/d5a2be3b148b51b8753ec7713cf697ae0eae14ed))
- **(logging)** Add safe Loki log preview ([9ec3aba](https://github.com/orkestra-cc/orkestra/commit/9ec3abaec9750c085c172a076d32939bf2417662))
- **(logging)** Expose diagnostic operations ([eb404c0](https://github.com/orkestra-cc/orkestra/commit/eb404c0b2e06498f2acd160c5c4edb3c23ec7b48))
- **(logging)** Apply permanent levels atomically ([1eaeb28](https://github.com/orkestra-cc/orkestra/commit/1eaeb283c80cfecb180b4bcbe1d400596686ca2c))
- **(logging)** Add expiring diagnostic overrides ([277db7f](https://github.com/orkestra-cc/orkestra/commit/277db7f8a8f8447cbe124e20e9aa4c1d9c386771))
- **(compliance)** Declare config groups; gate only retention_years ([8b1173f](https://github.com/orkestra-cc/orkestra/commit/8b1173fb2f78cbb35ee4a4096c419c913578c649))
- **(tenant)** Split provisioning config onto the per-tier rail ([bb97fd1](https://github.com/orkestra-cc/orkestra/commit/bb97fd1c0839e11bd2b40cedffb77ab8eb54e461))
- **(frontend-admin)** Service accounts admin UI ([8204e0a](https://github.com/orkestra-cc/orkestra/commit/8204e0a7ce9063cc96299c0c31a4b11a56b2639c))
- **(auth)** Service accounts with OAuth2 client-credentials grant (ADR-0014) ([3a15282](https://github.com/orkestra-cc/orkestra/commit/3a15282ed719ce4ef94d2b7334dd9dd0c988046e))
- **(auth)** Expose revocation store degradation ([6328821](https://github.com/orkestra-cc/orkestra/commit/6328821f224972aa6a2dbe4dec446e030b62e867))
- **(docker)** Extend HOST_BIND_ADDRESS to client-frontend, document the var ([2a6de05](https://github.com/orkestra-cc/orkestra/commit/2a6de052c2a6d870ad345e2ab6049e8a04c6cb86))
- **(docker)** Configurable host bind address for dev port mappings ([ac6403a](https://github.com/orkestra-cc/orkestra/commit/ac6403a86771ece8325e551650198eeac547d9d9))
- **(piiscan)** Flag fiscal identities as data-subject fields ([d7ef182](https://github.com/orkestra-cc/orkestra/commit/d7ef182f9fab69c1b20572d3e717c64c43e2d8b9))
- **(frontend-admin)** Real content on the /user/dashboard landing ([7d3c251](https://github.com/orkestra-cc/orkestra/commit/7d3c2517893ab8290eea86b18df83c37f3288b69))
- **(notification)** Declare config group tree + enum providers ([9de453e](https://github.com/orkestra-cc/orkestra/commit/9de453edb547c7a45818d304172ccef4cafc0d2c))
- **(frontend-admin)** Slim the top band to 3.75rem ([912e63c](https://github.com/orkestra-cc/orkestra/commit/912e63c80701fd1084bcfd8f0a31f7e725547f4c))
- **(frontend-admin)** Harden the light theme — shell, a11y, status semantics, i18n ([4969f0f](https://github.com/orkestra-cc/orkestra/commit/4969f0f0b9facee779b1b52900082a91ea5cb870))

### Bug fixes

- **(logging)** Harden operator workflows ([30c6cc9](https://github.com/orkestra-cc/orkestra/commit/30c6cc98cd81efdc4ef27028c3a05f6a95f2c79c))
- **(logging)** Document constrained log preview filters ([f25e651](https://github.com/orkestra-cc/orkestra/commit/f25e651d781a35f6042ca8d7ceb8ae2fa621071e))
- **(observability)** Use JSX provider test wrapper ([bc9e22a](https://github.com/orkestra-cc/orkestra/commit/bc9e22a7e1aa9a377d4b689dff90331b0aef9d2d))
- **(observability)** Harden logging module workspace ([c67b857](https://github.com/orkestra-cc/orkestra/commit/c67b857fd362d90c40d9f1ca74cd41c03f188348))
- **(observability)** Expose durable module overrides ([f3e9cba](https://github.com/orkestra-cc/orkestra/commit/f3e9cbae957e8120560c14866316721580bea04b))
- **(logging)** Expose permanent module override ([f3a7f10](https://github.com/orkestra-cc/orkestra/commit/f3a7f105891dd73fd3c0da0ce5da33fb8bbaccc2))
- **(observability)** Pass Provider children as props ([ba50dff](https://github.com/orkestra-cc/orkestra/commit/ba50dff1a681b3a51278c7c634cef5330fa7cea1))
- **(logging)** Harden Loki preview protocol handling ([04e08ad](https://github.com/orkestra-cc/orkestra/commit/04e08ad26fa552faba0211b9cf61ad03012a4684))
- **(scripts)** Stop making the JWT private key world-readable (#256) ([f8da250](https://github.com/orkestra-cc/orkestra/commit/f8da250d67d1919f4bdc002d5dff5a7a00febbf2))
- **(frontend-client)** Generate runtime config.js from env on dev/staging ([8ca02cc](https://github.com/orkestra-cc/orkestra/commit/8ca02cc9fec907a75374653005f10b9acfc2c9b2))
- **(auth)** Audit refresh family tenant scope ([3c27f97](https://github.com/orkestra-cc/orkestra/commit/3c27f97e61ce91a0b5acc4c625e8847cbcf5c260))
- **(auth)** Close final hardening gaps ([cac7e64](https://github.com/orkestra-cc/orkestra/commit/cac7e6471e0cf7a4091334c3ea756f4e3c446d95))
- **(auth)** Document tier-scoped repository exemptions ([5d59916](https://github.com/orkestra-cc/orkestra/commit/5d59916107c25b3ab30f3228ce1e48a97dcec730))
- **(auth)** Enforce one-winner WebAuthn assertions ([6685e25](https://github.com/orkestra-cc/orkestra/commit/6685e25d8e99a2922387c6b36c258d8efa59fbdb))
- **(auth)** Close refresh and MFA replay races ([578d679](https://github.com/orkestra-cc/orkestra/commit/578d6790f6c495b4efdc4f05f6df2c3ff3f15288))
- **(auth)** Preserve session identity across token renewal ([ccb4366](https://github.com/orkestra-cc/orkestra/commit/ccb4366a8cf9a1af7dd7eb92c41537f3dd197282))
- **(auth)** Require canonical token session ids ([dbeae83](https://github.com/orkestra-cc/orkestra/commit/dbeae831f8655173104f487aee624b7f850f962e))
- **(auth)** Sanitize OAuth eligibility failures ([fff84ab](https://github.com/orkestra-cc/orkestra/commit/fff84ab3e8b58b76eaa5a0be5b826dd53b93d31b))
- **(auth)** Reject inactive OAuth users ([882ac0a](https://github.com/orkestra-cc/orkestra/commit/882ac0a34a38e0fad614895c47b7141140e18714))
- **(auth)** Harden logging safety regression ([ebadd60](https://github.com/orkestra-cc/orkestra/commit/ebadd60f2e0f8731a84c2caba9686163ca25b97a))
- **(auth)** Remove sensitive debug logging ([188ee6c](https://github.com/orkestra-cc/orkestra/commit/188ee6c67390fd8a6a3cbb42bfa89622045d0e42))
- **(scripts)** Health-check probes the configured bind address ([ff541d5](https://github.com/orkestra-cc/orkestra/commit/ff541d5b54d04369778d7e31972e52202c0884ab))
- **(backend)** Openapi-dump path count works without jq ([5e4fdbb](https://github.com/orkestra-cc/orkestra/commit/5e4fdbb446ca881c4bcd38606e84b447d68724a4))
- **(middleware)** Let webhook routes through the audience gate ([f3d2b7c](https://github.com/orkestra-cc/orkestra/commit/f3d2b7c3ea8aebefb1049dd7665ed90ba41a25e9))
- **(tenant)** Reject immutable fields in UpdateTenant's $set ([7e44a62](https://github.com/orkestra-cc/orkestra/commit/7e44a624b57410e64dab74f7b32fd2e918471e3f))
- **(tenant)** Validate parent tier on create; purge crypto-shreds soft-deleted tenants ([6e7e643](https://github.com/orkestra-cc/orkestra/commit/6e7e6431be05cda41cb4d2d924633066a35d1fcd))
- **(security)** Close cross-tenant IDORs, wire fine-grained perms, gate admin endpoints ([128fd93](https://github.com/orkestra-cc/orkestra/commit/128fd93b58901dbc830fd626ddb5027a6d1c8524))
- **(frontend-admin)** Tenant switcher survives refresh and re-pick ([85e44ad](https://github.com/orkestra-cc/orkestra/commit/85e44ad76e0f00dafdceeaf289e461ab05a2143c))
- **(openapiauth)** Report status and size on error paths, never the response body (#234) ([8a928c2](https://github.com/orkestra-cc/orkestra/commit/8a928c2bb5cb8164ae335d0c9c2da22c712adcaf))
- **(test)** Restore Web Storage under Node 25+ ([6f62103](https://github.com/orkestra-cc/orkestra/commit/6f62103ce5c642c41e8fc1a645a02908d87c4100))
- **(frontend-admin)** One tab primitive, tertiary pagination ([bd39167](https://github.com/orkestra-cc/orkestra/commit/bd3916780c03c8e916255d03b57d18279bd965e0))
- **(module-config)** Surface invalid and incomplete state on the operator ([025db0d](https://github.com/orkestra-cc/orkestra/commit/025db0dee3125053a6b3a0edaaa5542cdf2b8ec7))
- **(frontend-admin)** Seat the collapsed-mode logo on the top band ([1717707](https://github.com/orkestra-cc/orkestra/commit/1717707dbb8cfa1a84be09fa8867c5b155ceb8be))
- **(frontend-admin)** Match the collapsed rail→content gap to the expanded one ([a4650cf](https://github.com/orkestra-cc/orkestra/commit/a4650cfca62946dc19e266982ce6ed46159c4dc5))
- **(frontend-admin)** Give the top band breathing room from the content ([fa1ac2d](https://github.com/orkestra-cc/orkestra/commit/fa1ac2d6fecefda22b256546a58ea1820864b672))
- **(frontend-admin)** Restore collapsed-rail clearances broken by the topbar pull ([42f85c2](https://github.com/orkestra-cc/orkestra/commit/42f85c2e03a7f90f301d92eaaa2aa7729366fad2))
- **(frontend-admin)** Seal the rail↔topbar junction and keep the logo in the rail ([2331770](https://github.com/orkestra-cc/orkestra/commit/2331770e476d099f67badc4c8694e11044f31cf1))
- **(frontend-admin)** Paint the whole sidebar column, not just the collapse ([87399f9](https://github.com/orkestra-cc/orkestra/commit/87399f903a7309458dea1f09654ff54aabb61853))

### Style

- **(logging)** Format legacy redirect test ([70fe2e9](https://github.com/orkestra-cc/orkestra/commit/70fe2e90b5c2e622b7b7ea3eceeaf68e6da7d8e6))

### Refactor

- **(logging)** Move controls into module detail ([0982d85](https://github.com/orkestra-cc/orkestra/commit/0982d85dbb84a0a447d867227aa2dffc0eb54a67))

### Documentation

- **(observability)** Clarify manual preview workflow ([76731e3](https://github.com/orkestra-cc/orkestra/commit/76731e3e2582e48921084f9648498c1484e089b6))
- **(api)** Constrain logging preview parameters ([613390e](https://github.com/orkestra-cc/orkestra/commit/613390e7fa21c7262ca0e986697924f11c724f6e))
- **(api)** Refresh logging operations schema ([f2e4cf0](https://github.com/orkestra-cc/orkestra/commit/f2e4cf0879ae7d612c344c64092f4d62244c945b))
- **(observability)** Plan logging module workspace ([8b6af1e](https://github.com/orkestra-cc/orkestra/commit/8b6af1ed90777c58723c545921d425d32677e7fa))
- **(observability)** Design logging operations workspace ([85a987c](https://github.com/orkestra-cc/orkestra/commit/85a987c276c5333c333734b7ffe459bc943cc2c1))
- **(frontend-client)** Document the runtime config generation in CLAUDE.md ([ab3e216](https://github.com/orkestra-cc/orkestra/commit/ab3e216ebe411e67863a6a688c2e5e66ab7b0f54))
- Record phase-5 group trees and the compliance gating correction ([fd26bbe](https://github.com/orkestra-cc/orkestra/commit/fd26bbeb31cd2eb90dad0877703618d1c4eae80e))
- **(adr)** ADR-0012 — module configuration group contract (phase 6) ([19c06e4](https://github.com/orkestra-cc/orkestra/commit/19c06e4fa6787dea924b3b5ece1dc23a1ee5e38f))
- **(auth)** Correct OAuth config cache comment ([68b4be5](https://github.com/orkestra-cc/orkestra/commit/68b4be5cc1a1d8573adf38eb86e88e7d262e7957))
- **(auth)** Correct oauth callback guidance ([df36c9d](https://github.com/orkestra-cc/orkestra/commit/df36c9dfde5f5baed55232da082fe3a7d2e62ff3))
- **(auth)** Add hardening and key rotation guidance ([e029d7a](https://github.com/orkestra-cc/orkestra/commit/e029d7a44dd15465db653a5e6d155ad7f46706d8))
- **(security)** Plan auth hardening ([5a1e7d6](https://github.com/orkestra-cc/orkestra/commit/5a1e7d6d6cc000b7463b0a5b569f25070d2c1253))
- **(security)** Design auth hardening ([aaa9da7](https://github.com/orkestra-cc/orkestra/commit/aaa9da703778cbd70d08cdf27acff8bcbaa95614))
- **(site)** Recommend skip-ci on private-fork upstream sync merges ([efce9e8](https://github.com/orkestra-cc/orkestra/commit/efce9e8808cbe199c7e7b388d2cdcb290af5ee14))
- **(sdk)** State the checkout-planner error-path contract on the iface seam ([7fc4bab](https://github.com/orkestra-cc/orkestra/commit/7fc4babde8c281b5e3c14774c5550d32c1430166))
- **(test)** Record the Node 25 Web Storage shim in the test-infra table ([e0a4e49](https://github.com/orkestra-cc/orkestra/commit/e0a4e49a25b7e2a3b352a8797c470f22ff9db09b))
- **(frontend-admin)** Refresh the design.json sidecar against DESIGN.md ([8484f69](https://github.com/orkestra-cc/orkestra/commit/8484f69d5540031eb7f8975563bab134360b86d2))

### Tests

- **(observability)** Support frontend TypeScript target ([d5cf7f7](https://github.com/orkestra-cc/orkestra/commit/d5cf7f7263cd46c85a9d8e0395d39e53a5a21aae))
- **(auth)** Harden structured log leak checks ([76cb930](https://github.com/orkestra-cc/orkestra/commit/76cb930735a8351728f5ab84710c26a167c4085f))
- **(auth)** Isolate revocation telemetry ([08d8678](https://github.com/orkestra-cc/orkestra/commit/08d8678331fdf1d830c7b65815e8026cf85adecb))

### CI

- **(docs)** Fail the dispatch when DOCS_DISPATCH_TOKEN is missing ([3ae5777](https://github.com/orkestra-cc/orkestra/commit/3ae5777e6a6ea984a284c2af0dfbe3f401f2f6ab))
- **(actions)** CI_FULL variable gates image publish, badges, security cron ([9fe657f](https://github.com/orkestra-cc/orkestra/commit/9fe657fbbe83da9a22154195e006dec52b2e8224))
- **(actions)** Cut Actions spend — gate scans, cap artifacts, clean GHCR ([f0514c2](https://github.com/orkestra-cc/orkestra/commit/f0514c28608daec336f725169082debf7c573863))

### Chores

- **(analyzers)** Realign tenantscope line anchors; drop stale declared.unused suppressions ([33dfbbe](https://github.com/orkestra-cc/orkestra/commit/33dfbbe5430fa3c075026e0429e58c7cb369d8a4))
- **(security)** Go 1.25.13, x/mod v0.40.0, nanoid audit fix (#244) ([0a9d926](https://github.com/orkestra-cc/orkestra/commit/0a9d926433ba5a52255f1320283272a9d80f702a))

## [0.5.0] - 2026-08-07

### Features

- **(frontend-admin)** Neutral-cool light theme, frozen dark, and the design authority (#220) ([e0990f2](https://github.com/orkestra-cc/orkestra/commit/e0990f2d0abf14155813d043a34e7c4bf42a849f))
- **(admin)** Name the group that unblocks an empty dependency panel ([1693f6e](https://github.com/orkestra-cc/orkestra/commit/1693f6efc99e5688eaefeef3d400721bbcc43b5a))
- **(i18n)** Translate the auth module's configuration labels ([d2e23ec](https://github.com/orkestra-cc/orkestra/commit/d2e23eca9ad3bb1561f7f47822a4af32d16fc97e))
- **(auth)** Declare the configuration group tree ([29d0840](https://github.com/orkestra-cc/orkestra/commit/29d08406b16c19cd68326714162957533bb56bbe))
- **(sdk)** Let a config field depend on any of its conditions ([15345d7](https://github.com/orkestra-cc/orkestra/commit/15345d787457333fb327619941011f8c80c332cc))
- **(admin)** Make the rail navigate the whole module page ([5f26dec](https://github.com/orkestra-cc/orkestra/commit/5f26dec6ad4bbefb20fa09390cb6cf77ad63ab03))
- **(admin)** Save module config across groups from one sticky bar ([0b60d51](https://github.com/orkestra-cc/orkestra/commit/0b60d517572739f75c4cbe1d950943ab74eab214))
- **(admin)** Navigate module config through a vertical rail ([ae90970](https://github.com/orkestra-cc/orkestra/commit/ae9097025635f5f4a32547a94538bb02aa57521c))
- **(admin)** Build the module config form from backend metadata ([fb721fb](https://github.com/orkestra-cc/orkestra/commit/fb721fb7f019e6fde72178b6fa8a574bd3c19cf0))
- **(admin)** Resolve module config labels through i18n ([0feefc3](https://github.com/orkestra-cc/orkestra/commit/0feefc37e3cb184aea8a6d362f8dae61a4309eeb))
- **(admin)** Render module config from the group tree ([a41cafc](https://github.com/orkestra-cc/orkestra/commit/a41cafc304db818fb85b8830d36709b2c36ef9c4))
- **(admin)** Model the module config group tree and field visibility ([1ec2d70](https://github.com/orkestra-cc/orkestra/commit/1ec2d70f495f0deccdd5a58069bb369d7f630cb0))
- **(sdk)** Serve module config groups from the admin API ([3598d0c](https://github.com/orkestra-cc/orkestra/commit/3598d0c0c1d692413d9c5f0766038222ab64ed65))
- **(sdk)** Validate module config group and dependency declarations ([7bea1a9](https://github.com/orkestra-cc/orkestra/commit/7bea1a98008b858c156f9b352eac4021d711cc0e))
- **(sdk)** Add ConfigGroup, Condition, and per-field config metadata ([20ae9db](https://github.com/orkestra-cc/orkestra/commit/20ae9dba6ed10f65cc05f2d870df42f861394751))

### Bug fixes

- **(orkestra-sh)** Supply the health-check script the deploy has always called (#227) ([ac1cad4](https://github.com/orkestra-cc/orkestra/commit/ac1cad46595237bea00bf72f7a76ff95adf525d8))
- **(frontend-admin)** Module-config typography and contrast on scale (#221) ([fb9684e](https://github.com/orkestra-cc/orkestra/commit/fb9684e2c013e59cb591a30338d3a18b27aa9593))
- **(frontend-admin)** Restore CJS interop broken by the Vite 8 migration (#218) ([6d5d7a1](https://github.com/orkestra-cc/orkestra/commit/6d5d7a1a25a006dd2bb3477faffc60b82d208e51))
- **(docker)** Pin AIR to v1.67.1 and align Node to 24 (#210) ([77ffc19](https://github.com/orkestra-cc/orkestra/commit/77ffc198af5a806062a699286aa19a92c4d90895))
- **(frontend-admin)** Stop the test store from batching on an animation frame (#207) ([bd6a9bf](https://github.com/orkestra-cc/orkestra/commit/bd6a9bf75efc5d4c6b0a8bc346a536040e728bab))
- **(docker)** X-XSS-Protection 0, and security headers in the images' own nginx configs (#199) ([ab4c6cb](https://github.com/orkestra-cc/orkestra/commit/ab4c6cb7aa41d42faa1bf211dcc0bae601ebe6fe))
- **(docker)** Restore nosniff on static assets, lost to add_header inheritance (#198) ([104dcc7](https://github.com/orkestra-cc/orkestra/commit/104dcc781441a64d92a2bbd85e54146aab350ddf))
- **(docker)** Never cache config.js — it pinned apiUrl for a year (#197) ([2f0c9ae](https://github.com/orkestra-cc/orkestra/commit/2f0c9aea45ab5d8ae7c0b5974aacd153ff53feef))
- **(admin)** Table a11y hardening + module-config polish batch (#195) ([ea7c52c](https://github.com/orkestra-cc/orkestra/commit/ea7c52c7866803b002854bd1542383807a8882f3))
- **(admin-modules)** Register config fields under RHF-safe names ([f86f765](https://github.com/orkestra-cc/orkestra/commit/f86f765ba9268af811940f5fb6bc8be74cee7a1b))
- **(admin)** Stop the compiled theme drifting against prettier ([fe318c0](https://github.com/orkestra-cc/orkestra/commit/fe318c0200ab931112f790024a458e2d0bbcf8a8))
- **(auth)** State the oauth group/toggle mapping the right way round ([f312ff7](https://github.com/orkestra-cc/orkestra/commit/f312ff7681e0b8a7a2faa8e0e57d9ed0500eca40))
- **(i18n)** Correct five Italian slips in the auth config labels ([5d7c799](https://github.com/orkestra-cc/orkestra/commit/5d7c79900675fb3c40589e365c1f6ff9c2135d48))
- **(i18n)** Correct Italian wording in auth config translations ([77e7a58](https://github.com/orkestra-cc/orkestra/commit/77e7a58fbf6f39a94ecd4629168f48c698dacfbf))
- **(auth,admin)** Render OAuth provider panels honestly when all-hidden ([616de04](https://github.com/orkestra-cc/orkestra/commit/616de04c4c0c6ef71b76e06ce57a568d6df3b97a))
- **(admin)** Stop the module detail page losing saves and edits in silence ([b101875](https://github.com/orkestra-cc/orkestra/commit/b1018754db3accc9e891da2ee2c06633d6096a8b))
- **(admin)** Validate stored module config values on load ([e77f2bb](https://github.com/orkestra-cc/orkestra/commit/e77f2bb31a9ac9e1e43231cf54b2ff738cb4985a))
- **(admin)** Give the module config blocker exactly one owner ([f7f5972](https://github.com/orkestra-cc/orkestra/commit/f7f5972614a937a1ba0c3d7da913a8b5196710df))
- **(admin)** Recompile theme.css/theme.rtl.css with the sticky save bar ([6005785](https://github.com/orkestra-cc/orkestra/commit/60057854b6a50ca0cbdb8adcc213fdc9d9b8609d))
- **(admin)** Validate only the save, clear secrets synchronously, fix blocker/degradation ([5b31e1b](https://github.com/orkestra-cc/orkestra/commit/5b31e1ba499775c6de6567a5e190e74b94fa7e33))
- **(admin)** Gate the Advanced toggle on visible fields, cover the real degradation path ([b9e9983](https://github.com/orkestra-cc/orkestra/commit/b9e99834b1b025765ac5d1b08f0717e089160a84))
- **(admin)** Stop the config form seeding a stored empty string as the default ([8c4492c](https://github.com/orkestra-cc/orkestra/commit/8c4492c86ed96953be36d1c730f03eec672f97da))
- **(admin)** Guard config descriptions on the resolved string, finish label association ([c1579ba](https://github.com/orkestra-cc/orkestra/commit/c1579ba039d81e23d3e2237335dcd5d7964336a9))
- **(admin)** Drop the half-built config tablist for the DOM it replaced ([4afe536](https://github.com/orkestra-cc/orkestra/commit/4afe536bb83cbc8d1a8658886f6872fcaedd2d92))
- **(admin)** Stop the console rejecting durations the backend accepts ([c6bdb67](https://github.com/orkestra-cc/orkestra/commit/c6bdb67c236a7bfdb1221bc8d810d742ccb6f564))
- **(admin)** Treat an explicit empty core-bundle label as not-found ([672989b](https://github.com/orkestra-cc/orkestra/commit/672989b4a52f6db51e2b66572016f3be58944465))
- **(admin)** Keep every config tab keyboard-reachable, drop dead configModal i18n keys ([39a2550](https://github.com/orkestra-cc/orkestra/commit/39a25507deaa6dd96f0fa9f7cf7ec1fa4d36a67e))
- **(docker)** Stop shipping a guessed trusted-proxy hop count for staging ([391a2c4](https://github.com/orkestra-cc/orkestra/commit/391a2c4b88ef166eb3450d6894110eb71b704055))
- **(logging)** Stop the boot banner claiming dev tokens are on in staging ([7e46e68](https://github.com/orkestra-cc/orkestra/commit/7e46e68d38730ea52b419b3dfe83e40dbd7e95af))
- **(config)** Parse the "d" duration suffix the config actually uses ([4830823](https://github.com/orkestra-cc/orkestra/commit/4830823a4cccc3e8e59596e2047948782739d0ef))
- **(auth)** Remediate 13 findings from the authentication audit ([f8a6da0](https://github.com/orkestra-cc/orkestra/commit/f8a6da0b4b03b9bec5ed1efe181107f692e65b9e))

### Refactor

- **(frontend-admin)** AdvanceTable + SubtleBadge for the last raw core tables (#196) ([ce2e011](https://github.com/orkestra-cc/orkestra/commit/ce2e01118cc491d784c7a2c882bfef641445d3d1))
- **(admin-modules)** Fail loudly on a missing register name ([452e5a6](https://github.com/orkestra-cc/orkestra/commit/452e5a603b7cbda3413d06cdfacdf6dc1479eeed))
- **(sdk)** Rename Condition to FieldCondition and harden config validation ([e1f7a54](https://github.com/orkestra-cc/orkestra/commit/e1f7a5471a3cf7ca6c212ca1d744ae944556a710))

### Documentation

- Repoint prose references to the SDK's post-ADR-0006 home (#229) ([ff95a87](https://github.com/orkestra-cc/orkestra/commit/ff95a87c957cafc6012c22c881ab26f929f5d79b))
- Repoint broken relative links, mostly ADR-0006 fallout (#228) ([582f4d6](https://github.com/orkestra-cc/orkestra/commit/582f4d65948ba65eb8a77544c077c9b4eec6177e))
- **(site)** Diagram the fork-chain directions and the push guard (#223) ([8a47e8c](https://github.com/orkestra-cc/orkestra/commit/8a47e8c972366d29baafbd0eec5b454e9eadf6c3))
- **(mobile)** State the real Dart floor, missed by the toolchain bump (#202) ([6ecd5e8](https://github.com/orkestra-cc/orkestra/commit/6ecd5e8bcc7c4b8302aac76da3613d659d078ac0))
- **(plans)** Warn phase 5 that RHF reads dotted config keys as paths ([083ac5c](https://github.com/orkestra-cc/orkestra/commit/083ac5c8c9352087a7b2856122a361a6ccdbbd46))
- **(sdk,auth)** Describe both DependsOnMatch modes where forks are told to look ([dba08da](https://github.com/orkestra-cc/orkestra/commit/dba08da8e9f6743852d4197159c1021ea7626aab))
- Record auth's config group tree and the any-match rule ([25e1438](https://github.com/orkestra-cc/orkestra/commit/25e1438283eb63a0651a823c883f5e77dd9cc0d9))
- **(plans)** Correct two wrong claims in the phase-4 plan ([b674a12](https://github.com/orkestra-cc/orkestra/commit/b674a12d65d0c75440e40dc8ce7ed4e4daaca613))
- **(plans)** Add the phase-4 plan for migrating auth ([d054f77](https://github.com/orkestra-cc/orkestra/commit/d054f77499a2c8c68758c234b88cdce4abc365c9))
- **(admin)** Record the module detail changes a fork trips over on sync ([3d20c40](https://github.com/orkestra-cc/orkestra/commit/3d20c40b881021fbcb78595b1989ee813e764387))
- Describe the module settings rail for addon authors ([6f3630a](https://github.com/orkestra-cc/orkestra/commit/6f3630aa753fef264621be47b54c8966ba3655d6))
- **(plans)** Correct the useModuleConfigForm signature in the phase-3 plan ([3117ea5](https://github.com/orkestra-cc/orkestra/commit/3117ea5426824163666d3a2db7f13c40b099318a))
- **(plans)** Add the phase-3 plan for the master-detail settings layout ([7e6f257](https://github.com/orkestra-cc/orkestra/commit/7e6f25762b66b1e08b4064406226eb2b736617e9))
- **(plans)** Drop an early return that would make a phase-2 test vacuous ([cdf5192](https://github.com/orkestra-cc/orkestra/commit/cdf519229cb9d7a3f2e307878bb65ed391530f2f))
- **(plans)** Add the phase-2 plan and move the RHF migration to phase 3 ([8695333](https://github.com/orkestra-cc/orkestra/commit/86953333b6b8f0c2319707ad570b37e701b1815c))
- **(plans)** Update Condition references to FieldCondition ([be4847e](https://github.com/orkestra-cc/orkestra/commit/be4847e11643d7333549543c3699f7d009f14e63))
- **(sdk)** Correct the sub-interface count and the BaseModule exception ([7f41576](https://github.com/orkestra-cc/orkestra/commit/7f41576e63f853b3a27cd20d10b1cecaba4edc60))
- **(plans)** Tighten phase-1 test design before execution ([808952b](https://github.com/orkestra-cc/orkestra/commit/808952b18bdf9e095697691e329ad2dec44136ff))
- **(plans)** Add the task-level plan for module-config phase 1 ([3c010fa](https://github.com/orkestra-cc/orkestra/commit/3c010faed1a01dde3f3e0fb87e49bcb73603c3dd))
- **(plans)** Split module-config work across the fork chain ([6553789](https://github.com/orkestra-cc/orkestra/commit/6553789d732fdbb2531222f1f306ea2465c0c508))
- **(plans)** Spec grouped module config + master-detail settings page ([de5841c](https://github.com/orkestra-cc/orkestra/commit/de5841c787696c9c836d86ee8cfda5aecc1b75a4))

### Tests

- **(server)** Assert auth's config groups resolve through the catalog ([2209ccd](https://github.com/orkestra-cc/orkestra/commit/2209ccde93e5ac3156bc1265eff0748d716193c8))
- **(server)** Gate module config declarations across the real catalog ([d5c56c1](https://github.com/orkestra-cc/orkestra/commit/d5c56c116ccc8b39b4cd60bb52d30e31be911a52))

### Build

- **(frontend-admin)** Migrate to Vite 8 (rolldown) with Vitest 4 (#206) ([5ebc9d5](https://github.com/orkestra-cc/orkestra/commit/5ebc9d5e225604a6c643680bf5f8388c2a3968cc))

### CI

- **(docs)** Dispatch docs.orkestra.cc rebuilds from pushes to main (#225) ([11670bd](https://github.com/orkestra-cc/orkestra/commit/11670bdb7c031cee114aaad976e5fe75dd5bb150))

### Dependencies

- **(deps)** Bump flutter_riverpod from 3.3.2 to 3.4.2 in /mobile (#191) ([2b3fbe6](https://github.com/orkestra-cc/orkestra/commit/2b3fbe6e6a5cf206af84836ca4eca2b471a927ba))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/config in /backend (#187) ([3aec534](https://github.com/orkestra-cc/orkestra/commit/3aec53494b26353d268abbfca9bd7558f75f7ed4))
- **(deps)** Bump github.com/aws/smithy-go in /backend (#188) ([976826b](https://github.com/orkestra-cc/orkestra/commit/976826bf8b01bf82ae40aa8a93918761db031edf))
- **(deps)** Bump dio from 5.10.0 to 5.11.0 in /mobile (#192) ([0123d2f](https://github.com/orkestra-cc/orkestra/commit/0123d2f56671b25cfed8886b8f643126dd254951))
- **(deps)** Bump @fortawesome/react-fontawesome (#182) ([7460d32](https://github.com/orkestra-cc/orkestra/commit/7460d320c153a5b91384b8900189168f0256f7c1))
- **(deps)** Bump react-dom from 19.2.7 to 19.2.8 in /frontend-admin (#186) ([58b6c4e](https://github.com/orkestra-cc/orkestra/commit/58b6c4ef2d384340ed7f7fa2e1fe74baeb8dc1d7))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2 in /backend (#189) ([f450f43](https://github.com/orkestra-cc/orkestra/commit/f450f43032628e043e373d6c724f7f96d595fb0a))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/service/s3 in /backend (#190) ([2ab3098](https://github.com/orkestra-cc/orkestra/commit/2ab30983f76c807229d86806d53a3f35bf8b0984))
- **(deps)** Bump github.com/prometheus/client_golang in /backend (#181) ([9389316](https://github.com/orkestra-cc/orkestra/commit/9389316dfffcf75197e25dfcc5f7e9011a8bd063))

### Chores

- **(frontend-client)** Sync the lockfile version the v0.4.0 bump missed ([6998806](https://github.com/orkestra-cc/orkestra/commit/69988064e09346d731d3cddc2ff30eaea67906c5))
- Docker/login-action v4, and drop the unused go_router dependency (#205) ([830ecd6](https://github.com/orkestra-cc/orkestra/commit/830ecd6300c0cebe41c9a64974159267ad2cc60c))
- **(deps-dev)** Prettier 3.8.4 -> 3.9.4 and reformat what it changes (#204) ([d856be1](https://github.com/orkestra-cc/orkestra/commit/d856be1d2a53682e441af069d1351bfa1d0cb94b))
- **(toolchain)** Flutter 3.35 -> 3.44, unblocking the mobile dependency floor (#201) ([e938997](https://github.com/orkestra-cc/orkestra/commit/e938997255c151bb60f81ef165ccc9e0d5769378))
- **(deps-dev)** Bump sass from 1.97.1 to 1.102.0 in /frontend-admin (#184) ([ed52a9a](https://github.com/orkestra-cc/orkestra/commit/ed52a9a9b989783fe3b0b05e2a926adcc9c9641b))
- **(frontend-admin)** Fix eslint/prettier formatting drift in vite.config.js ([b19df08](https://github.com/orkestra-cc/orkestra/commit/b19df0842b4a9e6af0f80d1364a07a7eb97c7d2c))
- **(frontend-admin)** Sync package-lock to 0.4.0 ([f46bb39](https://github.com/orkestra-cc/orkestra/commit/f46bb39ac7de306cd9da7496444c418504edce21))

## [0.4.0] - 2026-07-15

### Features

- **(storage)** Signed-GET download presigner + dual-endpoint split; fix pagination:false tables (#194) ([21a69a2](https://github.com/orkestra-cc/orkestra/commit/21a69a2b3334aaa2540e4f2db8276eb641ff850a))
- **(storage)** Object-storage foundation — per-domain buckets, presigned upload, SDK seam (ADR-0011) (#179) ([d24d981](https://github.com/orkestra-cc/orkestra/commit/d24d9815c6b4e84af734befd6e17b8bba9a9fd37))
- **(docker)** Write footer identity fields into runtime config.js ([c7bcdcd](https://github.com/orkestra-cc/orkestra/commit/c7bcdcd56778dca775bc698ffd82a4de6bb738df))
- **(orkestra.sh)** Export ORKESTRA_CLONE_VERSION + ORKESTRA_BUILD_COMMIT ([1befe88](https://github.com/orkestra-cc/orkestra/commit/1befe88ed39107af357e837f0e9a27aacfc1689c))
- **(frontend-admin)** Render deployment fingerprint in footer ([8716b14](https://github.com/orkestra-cc/orkestra/commit/8716b141215ec5347cfc267ca43192fe40937ff9))
- **(frontend-admin)** Add footer fields to runtime config ([097ff82](https://github.com/orkestra-cc/orkestra/commit/097ff828fdcfc1c4533409e7597420c8d03362a7))

### Bug fixes

- **(docker)** Probe client-frontend health on 127.0.0.1, not localhost ([ce2d5f0](https://github.com/orkestra-cc/orkestra/commit/ce2d5f062d65b2c1005f60a33e9eb62a1e7aa6c1))
- React-router 8 migration (both SPAs) + backend CVE dependency bumps (#193) ([ac766da](https://github.com/orkestra-cc/orkestra/commit/ac766da056e5d1163845e35149ea35936bfdd3d9))
- **(api)** Register health probes once for huma v2.39 (bump v2.34.1→v2.39.0) (#180) ([d3a9aa8](https://github.com/orkestra-cc/orkestra/commit/d3a9aa8eae0a7e6795a8528e3e3947794e7f7479))
- **(docker)** Make staging frontend-admin host allowlist env-driven ([4f630a5](https://github.com/orkestra-cc/orkestra/commit/4f630a54e923f00b0d667b3ab681f73eea0d1a59))
- **(version)** Exclude clone tags from base-version git describe ([c610d64](https://github.com/orkestra-cc/orkestra/commit/c610d6421923ea486eb8eb02fe2104e0f45939b4))
- **(docker)** Thread real apiUrl/wsUrl into prod runtime config.js ([fd454b6](https://github.com/orkestra-cc/orkestra/commit/fd454b6755e5004b05d773bb13e576147fbf6117))
- **(mobile)** Revert build_runner to ^2.6.1 (2.15.1 needs Dart 3.10+) ([0f4e953](https://github.com/orkestra-cc/orkestra/commit/0f4e953596ca0b12859caa7e026c8b90c393e967))

### Style

- **(frontend-admin)** Fix prettier formatting in environment.test.ts ([6941f72](https://github.com/orkestra-cc/orkestra/commit/6941f7252e6d844d588e012d618e19576193c273))

### Refactor

- **(frontend-admin)** Slim footer to a small monospace fingerprint ([1d84593](https://github.com/orkestra-cc/orkestra/commit/1d845933638967538f9533b27f4c904feda4efd6))

### Documentation

- **(docker)** Correct infra healthcheck, ports, and per-stack framing ([8297ad2](https://github.com/orkestra-cc/orkestra/commit/8297ad22fdbc0adc19cb96b506abff3730a57f0c))
- **(adr)** ADR-0010 D5 (Prop: trailer) + D6 (selective downstream) ([a6f9671](https://github.com/orkestra-cc/orkestra/commit/a6f96718efe80970216166f5abb3a85322f4d11e))

### CI

- **(release)** Authenticate git-cliff's GitHub API calls ([50ce0e0](https://github.com/orkestra-cc/orkestra/commit/50ce0e0087f80d43bb0d8c8dc2f01f516ad7dc89))

### Dependencies

- **(deps)** Bump github.com/aws/aws-sdk-go-v2/service/s3 in /backend (#174) ([0b7ffa5](https://github.com/orkestra-cc/orkestra/commit/0b7ffa5338ac5f1084589dae838ac4323ed7c175))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/config in /backend (#176) ([28098d0](https://github.com/orkestra-cc/orkestra/commit/28098d096e5f27fc626edc752f33e7f69529e636))
- **(deps)** Bump actions/setup-go from 6 to 7 (#171) ([fe240ba](https://github.com/orkestra-cc/orkestra/commit/fe240bae0b978e1841ceacf1792d5ad8919c1ea8))
- **(deps)** Bump actions/setup-node from 6 to 7 (#172) ([79cb4d5](https://github.com/orkestra-cc/orkestra/commit/79cb4d5f73b0ddd005d6bddc23ce08d2dc493875))
- **(deps)** Bump react-icons from 5.5.0 to 5.7.0 in /frontend-admin (#163) ([999f3ae](https://github.com/orkestra-cc/orkestra/commit/999f3aecf64c3cb504b1d51778eea76c839f9871))
- **(deps)** Bump @reduxjs/toolkit in /frontend-admin (#164) ([f89f073](https://github.com/orkestra-cc/orkestra/commit/f89f073b85ceb635481ef5c86f423f358da70c7d))
- **(deps)** Bump fuse.js from 7.1.0 to 7.4.2 in /frontend-admin (#166) ([4b4ef3c](https://github.com/orkestra-cc/orkestra/commit/4b4ef3ce618b343fae2ed75ee146af8bfe055ae8))
- **(deps)** Bump the fortawesome group across 1 directory with 6 updates (#173) ([fe9104d](https://github.com/orkestra-cc/orkestra/commit/fe9104d9b28636c527608dec4552a9b25af5c0da))
- **(deps)** Bump github.com/go-chi/chi/v5 in /backend (#161) ([72be338](https://github.com/orkestra-cc/orkestra/commit/72be338a9b5b0c554638e6bede031e5be2170d8e))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/credentials in /backend (#178) ([83fd4aa](https://github.com/orkestra-cc/orkestra/commit/83fd4aaad32e4adb19c0d42dd11fd8e3a10ddd94))

## [0.3.15] - 2026-07-10

### Features

- **(setup)** Let the wizard OrgStep be skipped (zero-tenant install) ([d86a50a](https://github.com/orkestra-cc/orkestra/commit/d86a50aa537189927b20e23724743c25d5a3f973))
- **(setup)** Stop auto-bootstrapping an internal tenant at install ([67c38d8](https://github.com/orkestra-cc/orkestra/commit/67c38d8280e53dc4f03e1a4c4f42e22ab45dc1aa))
- **(store)** Tenant-scoped-tag registry for addon-owned cache tags ([65f9ff5](https://github.com/orkestra-cc/orkestra/commit/65f9ff5f8014ed9831c9ba328f6e8ac7db86427c))

### Bug fixes

- **(frontend-admin)** Align @fortawesome/* to 7.3 to unbreak tsc ([71e3c97](https://github.com/orkestra-cc/orkestra/commit/71e3c97facc536c565a33e9568bbb4bd70d7f7c6))
- **(dev)** Host-only refresh cookies so LAN-IP / hostname access persists ([bdcbb7a](https://github.com/orkestra-cc/orkestra/commit/bdcbb7ab121f0d540cd564810f2c4d47dd8e47da))
- **(navigation)** Show operator menus when acting without a tenant ([575fa7c](https://github.com/orkestra-cc/orkestra/commit/575fa7cf3029c988bd6341bd0353a3076fe985f6))

### Refactor

- **(modules)** Auto-discover addon manifests via glob ([882bf9d](https://github.com/orkestra-cc/orkestra/commit/882bf9d417b4cace59953b8f66e0d7b483242de8))

### Tests

- **(setup)** Cover OrgStep create path; guide skip Finish copy to Internal Tenants ([3297781](https://github.com/orkestra-cc/orkestra/commit/32977810102fe06929f5c638ccaf667dcb515066))

### Build

- **(deps)** Bump Go 1.25.11 → 1.25.12 to clear govulncheck gate ([b3f1702](https://github.com/orkestra-cc/orkestra/commit/b3f1702636d410e9291481f051ceec3d77ffe3f0))

### CI

- **(security)** Read the shared allowlist in the Security Scan gate ([4c90e0d](https://github.com/orkestra-cc/orkestra/commit/4c90e0de8b488afc3c80a4dfbd4bff2cb3a339ce))
- **(dependabot)** Target the dev integration branch ([469eb54](https://github.com/orkestra-cc/orkestra/commit/469eb5449d9adffc3370c617d8e1fe0d0f468fcd))

### Dependencies

- **(deps)** Bump github.com/aws/aws-sdk-go-v2/config in /backend ([0a4ba44](https://github.com/orkestra-cc/orkestra/commit/0a4ba44748536f86393b17df7bd1ea12c15e9a1e))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/service/s3 in /backend ([584cdd3](https://github.com/orkestra-cc/orkestra/commit/584cdd3df5275953c354f3144d283bf2a6336c41))
- **(deps)** Bump golang.org/x/tools from 0.47.0 to 0.48.0 in /backend ([4ac0e44](https://github.com/orkestra-cc/orkestra/commit/4ac0e441e7a0627c17ec8707535e9da85afe31c6))
- **(deps)** Bump golang.org/x/crypto from 0.53.0 to 0.54.0 in /backend ([fbc1f07](https://github.com/orkestra-cc/orkestra/commit/fbc1f078762d552eab08ed372a668b4895109f65))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/credentials in /backend ([ee02baf](https://github.com/orkestra-cc/orkestra/commit/ee02baf17cf5eaa5f6a9c48067bacc8a84239ca2))
- **(deps)** Bump @fortawesome/free-regular-svg-icons in /frontend-admin ([72851ae](https://github.com/orkestra-cc/orkestra/commit/72851ae9d010997c962c09d4cd275f00aff4701c))
- **(deps)** Bump @rollup/rollup-win32-x64-msvc in /frontend-admin ([b3b5e36](https://github.com/orkestra-cc/orkestra/commit/b3b5e36323446804cc46395835bc9bed296ed1a4))
- **(deps)** Bump dompurify from 3.4.2 to 3.4.11 in /frontend-admin ([ec5e858](https://github.com/orkestra-cc/orkestra/commit/ec5e85847be6d38aaf385593e4d07e110e918733))
- **(deps)** Bump build_runner from 2.6.1 to 2.15.1 in /mobile ([e64126c](https://github.com/orkestra-cc/orkestra/commit/e64126cd1b7621e016f0c1b7d86599ad1a299065))
- **(deps)** Bump i18next from 23.16.8 to 26.3.6 in /frontend-admin ([4c08163](https://github.com/orkestra-cc/orkestra/commit/4c0816304cc821ce496aa96d9b120aab9dd0c5e8))
- **(deps)** Bump uuid from 13.0.2 to 14.0.1 in /frontend-admin ([b18b933](https://github.com/orkestra-cc/orkestra/commit/b18b9336ceb7f50badfa2daf4f6b328d4c6efdc5))

## [0.3.14] - 2026-07-06

### Core

- Catch up to commons dev — multi-stack isolation + env-setup-wizard + storage-endpoint fix (#143) ([dde83ef](https://github.com/orkestra-cc/orkestra/commit/dde83ef3630b1105e7604214061cdac551a82edd))

## [0.3.13] - 2026-07-04

### Documentation

- **(adr)** Add ADR-0010 commons fork chain + private-forks guide ([529b8fd](https://github.com/orkestra-cc/orkestra/commit/529b8fd37b41c9817cd2d98e50d0f7194d82d5e1))

## [0.3.12] - 2026-07-03

### Features

- **(frontend-admin)** Add icons to Developer realm submenu groups ([bf397d3](https://github.com/orkestra-cc/orkestra/commit/bf397d3f32fbbf7f3ca57c4d52392e90be35f392))
- **(i18n)** Rename Internal/External Tenants to Gruppi interni/esterni (IT) ([29c1ae0](https://github.com/orkestra-cc/orkestra/commit/29c1ae036ec814c09e5afaf7ffc0c773cc1bae1d))
- **(tenant)** Group External Tenants under the Administration realm ([4b2e40b](https://github.com/orkestra-cc/orkestra/commit/4b2e40b63ac19b1af4d49a84f09d4fcbe9ddcfb2))

### Bug fixes

- **(frontend-admin)** Cache-bust theme CSS and tune vertical navbar ([1eaf114](https://github.com/orkestra-cc/orkestra/commit/1eaf11477c5ef964ba9fa40ea8e2c6305c1b8e64))
- **(container)** Build-tag the Docker infra manager off by default (#137) ([9fbf48f](https://github.com/orkestra-cc/orkestra/commit/9fbf48f71be23147e764d9140b21b05ad40e7a1a))

### Style

- **(frontend-admin)** Tighten vertical navbar spacing and icon rail ([cb3f174](https://github.com/orkestra-cc/orkestra/commit/cb3f174fc00304512952334ceaacde74dbe698ef))
- **(frontend-admin)** Tighten sidebar toggle→logo gap to 0.75rem ([492a19d](https://github.com/orkestra-cc/orkestra/commit/492a19d29a32fd6e27ff9bb2ba39de6e5d5e3391))
- **(frontend-admin)** Trim trailing whitespace in theme CSS ([ff24e07](https://github.com/orkestra-cc/orkestra/commit/ff24e07bead35c2db6f44cc4f7c091ead3764774))
- **(theme)** Recompile CSS for RTL stat-ribbon parity ([1aa3490](https://github.com/orkestra-cc/orkestra/commit/1aa3490cd6985c4bbb5faaae8f6d18bf32e2e9fd))

### Documentation

- **(skills)** Rename and rewrite api-test + docker skills ([c0f2d97](https://github.com/orkestra-cc/orkestra/commit/c0f2d97e4a56fe236d7413516b07120c8db56580))
- **(skills)** Rename frontend-design skill to orkestra-frontend-admin ([808eef7](https://github.com/orkestra-cc/orkestra/commit/808eef7802d176a3bfeec7c78c26de6413712d9c))
- **(skills)** Rename and refresh mongo-collection-naming skill ([51cda8a](https://github.com/orkestra-cc/orkestra/commit/51cda8a3c37a23b9074162bc498e2d02ec411a79))
- **(frontend-design-skill)** Fix trigger and enforce pre-flight reads ([2bb9c62](https://github.com/orkestra-cc/orkestra/commit/2bb9c628029bca4c71ab047d64702fdc11177292))

### CI

- Make coverage-badge push race-proof (reset-to-remote + retry) (#129) ([60a3e56](https://github.com/orkestra-cc/orkestra/commit/60a3e5673cc6606e4bffaaa0d270c628bd79d33f))

### Dependencies

- **(deps)** Bump github.com/aws/aws-sdk-go-v2/credentials in /backend (#141) ([3f721c7](https://github.com/orkestra-cc/orkestra/commit/3f721c7616bbf63743eee6fc4d63ad88290669e0))
- **(deps)** Bump golang.org/x/tools from 0.45.0 to 0.47.0 in /backend (#142) ([f1d675c](https://github.com/orkestra-cc/orkestra/commit/f1d675c96f9b2b99ba0f27a92a127cbd4465ee10))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/service/s3 in /backend (#139) ([2514308](https://github.com/orkestra-cc/orkestra/commit/251430834093f9b56e9f677fbc5faedc47db3bad))
- **(deps)** Bump dio from 5.9.2 to 5.10.0 in /mobile (#138) ([4c1f096](https://github.com/orkestra-cc/orkestra/commit/4c1f096b09d9fdc1163f0066a5a87f834f891b15))
- **(deps)** Bump @testing-library/react in /frontend-admin (#136) ([bb36392](https://github.com/orkestra-cc/orkestra/commit/bb36392036cb2cc46cb7bb776a74bf7e2444cc28))
- **(deps)** Bump @fortawesome/react-fontawesome in /frontend-admin (#134) ([49d6519](https://github.com/orkestra-cc/orkestra/commit/49d65196c20dd8053d4388b599465153b9d3dbde))
- **(deps)** Bump react-dom from 19.2.3 to 19.2.7 in /frontend-admin (#133) ([35b6041](https://github.com/orkestra-cc/orkestra/commit/35b6041483a4470ec2e24245d02633b9f3d095e7))
- **(deps)** Bump @fullcalendar/list in /frontend-admin (#132) ([7651cb1](https://github.com/orkestra-cc/orkestra/commit/7651cb1a53895b151de34d4ddbb8a7c81a8fcd76))
- **(deps)** Bump github.com/redis/go-redis/v9 in /backend (#130) ([b6a0fad](https://github.com/orkestra-cc/orkestra/commit/b6a0fad244a785e1b21601d343f1ba54e3f91287))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/service/s3 in /backend ([6bcd50a](https://github.com/orkestra-cc/orkestra/commit/6bcd50a913cf8f626a6a80ff6ca275dac30027d8))
- **(deps)** Bump @fullcalendar/bootstrap in /frontend-admin ([87d59e2](https://github.com/orkestra-cc/orkestra/commit/87d59e2f39fa0e9f6b97d16d0af68aff4ff7f6a1))
- **(deps)** Bump react-router-dom in /frontend-admin ([4326b0d](https://github.com/orkestra-cc/orkestra/commit/4326b0d37edb3b65869174d40fd301b1db8c38ed))
- **(deps)** Bump @rollup/rollup-win32-x64-msvc in /frontend-admin ([25cd006](https://github.com/orkestra-cc/orkestra/commit/25cd00660919acd683dd2e3699f5c9ac464d95a5))
- **(deps)** Bump @fullcalendar/daygrid in /frontend-admin ([5dfa0a6](https://github.com/orkestra-cc/orkestra/commit/5dfa0a6d104362c35228b4936d5948dfab144731))
- **(deps)** Bump go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp ([c71b365](https://github.com/orkestra-cc/orkestra/commit/c71b365a314263ff0bd51d7dc77f98cccfdeaf90))
- **(deps)** Bump github.com/redis/go-redis/v9 in /backend ([0134487](https://github.com/orkestra-cc/orkestra/commit/013448728324d23aacd56fa827c94fadbf8443de))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2/config in /backend ([5e134f7](https://github.com/orkestra-cc/orkestra/commit/5e134f7fb914fe3ccb98e023662e184c21a56579))
- **(deps)** Bump github.com/go-webauthn/webauthn in /backend ([f481ab3](https://github.com/orkestra-cc/orkestra/commit/f481ab3135dd74d6e82dcd4144611a0634662a47))
- **(deps)** Bump flutter_riverpod, freezed_annotation, json_serializable, build_runner and freezed ([5e46886](https://github.com/orkestra-cc/orkestra/commit/5e46886088d86239bec90f2af17f10593ed766ba))
- **(deps)** Bump actions/checkout from 6 to 7 ([e932fa0](https://github.com/orkestra-cc/orkestra/commit/e932fa0c2027a556fde70d17affd9bf84148acae))

## [0.3.11] - 2026-06-19

### Features

- **(navigation)** Truthful role-matrix audit + fix duplicate headers ([bae6278](https://github.com/orkestra-cc/orkestra/commit/bae627880896f126b16ddd5a7568626de5cee3f2))

### Refactor

- **(tenant)** Rename Clients nav to External Tenants, drop addon-only client tabs ([e7b3804](https://github.com/orkestra-cc/orkestra/commit/e7b38047be29ee1f0dbed37a63464b284444217a))

### Documentation

- Reflect compliance as the 8th core module ([bc46381](https://github.com/orkestra-cc/orkestra/commit/bc46381b4a5f6a07faf198ffd06715e67ce53f98))
- Refresh ADR statuses, archive shipped plans, fix stale refs ([f6b838b](https://github.com/orkestra-cc/orkestra/commit/f6b838b658086f08e24deab98611282b15abf7fa))

## [0.3.10] - 2026-06-19

### Features

- **(frontend-admin)** Render StatCard attention flag as a corner ribbon ([b8804ff](https://github.com/orkestra-cc/orkestra/commit/b8804ff7b0106f6b8be193715c75069d62fd8368))
- **(frontend-admin)** Share StatCard/SectionCard design-system primitives ([ca082f1](https://github.com/orkestra-cc/orkestra/commit/ca082f1585cf259bb13a1afd503ef36a74e37198))
- **(tenant)** Merge per-tier provisioning policy (manual default + install bootstrap) ([e4c28eb](https://github.com/orkestra-cc/orkestra/commit/e4c28ebce59c4d9b669d559978402cd50e99b0de))
- **(tenant)** Default provisioning to manual + bootstrap internal tenant at install ([c4075f9](https://github.com/orkestra-cc/orkestra/commit/c4075f92cc8aa35589bfbceb95815456fd8b1ddb))
- **(tenant)** Admin-managed per-tier provisioning policy ([7310f15](https://github.com/orkestra-cc/orkestra/commit/7310f15385c4a961420a7c591f520f2d8d3ed989))
- **(compliance)** Surface SOC2 evidence (nav gate + admin page) ([6b7408a](https://github.com/orkestra-cc/orkestra/commit/6b7408a8d9050980047731bde3646f624ef561dd))
- **(compliance)** Redesign the admin GDPR/compliance page UI ([357eb2a](https://github.com/orkestra-cc/orkestra/commit/357eb2a8abc7c05dc0bb1ac5f477a4801c36c618))

### Bug fixes

- **(notification)** Use bson.D for multi-key template list sort (#127) ([26e7d73](https://github.com/orkestra-cc/orkestra/commit/26e7d73c263e094d88bc0e07e0032e52437abd71))
- **(compliance)** Drop redundant nav section on admin items ([c2fcdef](https://github.com/orkestra-cc/orkestra/commit/c2fcdef95d31e5e3db94c1e0ba118918e57be20c))

### Documentation

- **(tenant)** Fix pre-existing inaccuracies in module CLAUDE.md ([475d768](https://github.com/orkestra-cc/orkestra/commit/475d768b6ab3a8d8b82dd7682a61776500ac5c57))
- **(claude)** Link the logging module's CLAUDE.md from the root module map ([d5dd7d2](https://github.com/orkestra-cc/orkestra/commit/d5dd7d25b3620254ae7f5ff2e4f0882fe3278343))
- **(logging)** Document the core logging module (ADR-0005 Phase F) ([8a60f5a](https://github.com/orkestra-cc/orkestra/commit/8a60f5a282a78033a0988bf8e427fcfaf0d38bea))
- **(compliance)** Document the core compliance module + fix stale module counts ([ab69423](https://github.com/orkestra-cc/orkestra/commit/ab6942310638b097cbbfcb699608812a41ffaf9c))

### Chores

- Merge main (#127 notification sort hotfix) into dev before v0.3.10 ([add7ad5](https://github.com/orkestra-cc/orkestra/commit/add7ad51422e6dd692e26da188afceff17e79114))
- **(docs)** Absorb logging module page + badge refresh into main ([f0d5838](https://github.com/orkestra-cc/orkestra/commit/f0d5838912afd062092dca25099defb12e28d5a3))

## [0.3.9] - 2026-06-18

### Features

- **(piiscan)** CI gate for subject-PII collections without a PIIProducer ([dcef19d](https://github.com/orkestra-cc/orkestra/commit/dcef19dfa766adc6d535116ca854ec1e856e704e))
- **(blob)** Add server-side Put to the blob.Store seam ([e1bf96a](https://github.com/orkestra-cc/orkestra/commit/e1bf96af64431a05695908195edcdb38963f968b))
- **(compliance)** Core GDPR data-subject-rights module (audit, DSR, legal hold, retention) ([8a948f3](https://github.com/orkestra-cc/orkestra/commit/8a948f3fb236eb44830f251430af2c70edb7c45b))

### Bug fixes

- **(authz)** Cedar-cover compliance system.*.manage permissions ([43dc34f](https://github.com/orkestra-cc/orkestra/commit/43dc34f3baed27df7b2b6b035f113dc893db5711))

### Documentation

- **(compliance)** Document PIIProducer registration in the 4 producing modules ([210d9e6](https://github.com/orkestra-cc/orkestra/commit/210d9e6985172bae2b89dd6269dc184a528e9b61))

### Tests

- **(compliance)** Cover services, handlers, repository, and frontend ([1ab1ddf](https://github.com/orkestra-cc/orkestra/commit/1ab1ddfa2baee4af7be50f19511a63df5f5d0fc6))

## [0.3.8] - 2026-06-14

### Features

- **(devtoken)** Let dev tokens carry an acting tenant ([33ecec5](https://github.com/orkestra-cc/orkestra/commit/33ecec54c3bfc955a4a50c9ac62d398e421eb148))

### Documentation

- **(adr)** Propose ADR-0008 — partition OpenAPI spec per module ([3783a74](https://github.com/orkestra-cc/orkestra/commit/3783a7446dd480dfbc6537f44e33b3a55b105529))

### Chores

- Absorb devtoken-tenant-aware (PR #112) into dev ([c0a7b56](https://github.com/orkestra-cc/orkestra/commit/c0a7b56601caad7ffe64a4a311896cec1d44f47b))

## [0.3.7] - 2026-06-14

### Features

- **(i18n)** Opt-in namespaced error-code resolver + client/scaffold docs (ADR-0007) ([7e686d0](https://github.com/orkestra-cc/orkestra/commit/7e686d0647698b12593c75a5da3eee8854495996))
- **(i18n)** Per-addon namespace seam so addon translations never touch core ([6832574](https://github.com/orkestra-cc/orkestra/commit/6832574116febce27c72654a8de1feb15626fbe0))

### Documentation

- **(skills)** Fix orkestra-addon cross-reference link paths ([ec2edfa](https://github.com/orkestra-cc/orkestra/commit/ec2edfa467a1394cd36742d72a5ee57caef16488))

### Chores

- **(skills)** Add orkestra-addon authoring skill (full-stack rules + checklist) ([3d1f3d6](https://github.com/orkestra-cc/orkestra/commit/3d1f3d6951612167d43097a904fb3a62fdb7b670))
- **(skills)** Add orkestra-stack lifecycle skill, cross-ref from docker ([e063af5](https://github.com/orkestra-cc/orkestra/commit/e063af50b174962d798dfe1b090c2d234cb9844e))

## [0.3.6] - 2026-06-14

### Features

- **(profile)** Show the user's group (tenant) memberships on their profile (#101) ([e6516f4](https://github.com/orkestra-cc/orkestra/commit/e6516f474f47b003323de71e0ca256701a96e4d4))
- **(tenant)** Sync authz bindings on member remove/role change + inline role editing (#99) ([7660964](https://github.com/orkestra-cc/orkestra/commit/766096428745c25dce9f405583491a00d442003c))

### Bug fixes

- **(admin)** Improve tenants-table readability (#100) ([b23e7d9](https://github.com/orkestra-cc/orkestra/commit/b23e7d97dd8fe46f67da5e2739ddd12f16990918))

### Dependencies

- **(deps)** Bump github.com/aws/aws-sdk-go-v2/credentials in /backend (#108) ([b1caaaf](https://github.com/orkestra-cc/orkestra/commit/b1caaaf9432251f0b3f4c1126f816d63cf9d13aa))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2 in /backend (#107) ([8b20c38](https://github.com/orkestra-cc/orkestra/commit/8b20c386f9651772827556ee233374ced4f5723c))
- **(deps)** Bump golang.org/x/crypto from 0.52.0 to 0.53.0 in /backend (#103) ([5ce2cb8](https://github.com/orkestra-cc/orkestra/commit/5ce2cb866517d4b4d47f1ef6850c7e0482c1adc5))
- **(deps)** Bump echarts from 6.0.0 to 6.1.0 in /frontend-admin (#105) ([ba30925](https://github.com/orkestra-cc/orkestra/commit/ba30925708222f314f0e7edafe11b183b6782de2))
- **(deps)** Bump github.com/aws/smithy-go in /backend (#102) ([c728c5e](https://github.com/orkestra-cc/orkestra/commit/c728c5eac6c9cdb89d8c34eff067e7bbc0348354))
- **(deps)** Bump github.com/go-playground/validator/v10 in /backend (#109) ([694bc48](https://github.com/orkestra-cc/orkestra/commit/694bc4806df673082ee8aca39c99928dac9a942b))
- **(deps)** Bump react-dropzone in /frontend-admin (#86) ([98f319b](https://github.com/orkestra-cc/orkestra/commit/98f319bd8b102b867c43395201d2c39e6c9d2bbc))
- **(deps)** Bump flutter_lints from 3.0.2 to 6.0.0 in /mobile (#94) ([c6770c0](https://github.com/orkestra-cc/orkestra/commit/c6770c0a876581c5dc8069ac486f264e59eadaa1))
- **(deps)** Bump dio from 5.9.0 to 5.9.2 in /mobile (#92) ([d2a8e4f](https://github.com/orkestra-cc/orkestra/commit/d2a8e4fef49ac1fc078f271330e87de8afd3a1f2))
- **(deps)** Bump github.com/cedar-policy/cedar-go in /backend (#93) ([710b372](https://github.com/orkestra-cc/orkestra/commit/710b372fc20703ddb246d8cb3bd587c90ad5c768))
- **(deps)** Bump github.com/go-chi/chi/v5 in /backend (#97) ([2d3c9a7](https://github.com/orkestra-cc/orkestra/commit/2d3c9a73bf6c949aaaaaea726a72ed96c70e15af))
- **(deps)** Bump go.opentelemetry.io/contrib/bridges/otelslog (#98) ([4323cf8](https://github.com/orkestra-cc/orkestra/commit/4323cf8b854c39c3195e4975c0ee780b1fc80ecd))
- **(deps)** Bump github.com/aws/aws-sdk-go-v2 in /backend (#96) ([aeb2123](https://github.com/orkestra-cc/orkestra/commit/aeb212396891df8c227685120284bbb1cd935e35))
- **(deps)** Bump github.com/redis/go-redis/v9 in /backend (#90) ([4443336](https://github.com/orkestra-cc/orkestra/commit/44433367c91ff1468e5f8c4833759b50dad5d1ba))
- **(deps)** Bump web-vitals from 5.2.0 to 5.3.0 in /frontend-admin (#88) ([0d2b544](https://github.com/orkestra-cc/orkestra/commit/0d2b544dbe1bd6b4deef1708232a0ba7fa655bcf))
- **(deps)** Bump docker/metadata-action from 5 to 6 (#85) ([56db54f](https://github.com/orkestra-cc/orkestra/commit/56db54fb06caa1a31c0a817b1a582421007c06ca))

### Chores

- **(deps-dev)** Bump prettier from 3.7.4 to 3.8.4 in /frontend-admin (#106) ([9b31608](https://github.com/orkestra-cc/orkestra/commit/9b316086810c46d04e1e0b12d75d2fb14f92a059))
- **(deps-dev)** Bump msw from 2.14.5 to 2.14.6 in /frontend-admin (#104) ([a71345d](https://github.com/orkestra-cc/orkestra/commit/a71345d8bda1ff1da077c6ce9dbfd879c794d205))
- **(deps-dev)** Bump @typescript-eslint/parser in /frontend-admin (#89) ([e061d72](https://github.com/orkestra-cc/orkestra/commit/e061d72da93ef66eaf611d72fcf1c2582b05ecb6))

## [0.3.5] - 2026-06-08

### Bug fixes

- **(deps)** Bump tinymce to ^8.6.0 to patch high-severity XSS advisories ([98b3446](https://github.com/orkestra-cc/orkestra/commit/98b3446aafec8151835ac47a4d678e86ee293e50))

## [0.3.4] - 2026-06-04

### Bug fixes

- **(auth)** Return to requested deep link after login ([16ab754](https://github.com/orkestra-cc/orkestra/commit/16ab7543592cb97ba3368c735e86ba3bd6edb5f1))

### Chores

- Back-merge origin/main into dev (v0.3.3 — Go 1.25.11 vuln bump, auth/admin-ui fixes) ([71c22e0](https://github.com/orkestra-cc/orkestra/commit/71c22e09852bc155ce90d08d9796cc7edcaf394b))

## [0.3.3] - 2026-06-04

### Bug fixes

- **(auth)** Let operator tokens switch acting tenant via X-Tenant-ID (#83) ([3449c8f](https://github.com/orkestra-cc/orkestra/commit/3449c8fa897585be0cdeaf9bdb8b3e6eca9e5547))
- **(admin-ui)** Stop impersonation when picking a workspace in NineDotMenu (#82) ([f83243a](https://github.com/orkestra-cc/orkestra/commit/f83243a9c3068c37cfbeb867551398d5064e24e5))
- **(deps)** Bump Go 1.25.10 → 1.25.11 for stdlib advisories (#84) ([42670c7](https://github.com/orkestra-cc/orkestra/commit/42670c728710cf906b119db14546dd5d333b0237))

## [0.3.2] - 2026-06-02

### Features

- **(admin-ui)** Server-side table pagination + SDK partial-filter index (#80) ([ebc2ef7](https://github.com/orkestra-cc/orkestra/commit/ebc2ef7798336d3d90c922774d00c8a8704dd6af))

### Bug fixes

- **(sdk)** UpdateConfig merges secrets instead of replacing them (#81) ([61a2903](https://github.com/orkestra-cc/orkestra/commit/61a2903de0bcc143b3ca53950458c6589a600892))
- **(navigation)** Gate Developer realm menu on developer role ([e3b2cd3](https://github.com/orkestra-cc/orkestra/commit/e3b2cd3001bddb27ba098b44878f91eaa92fd205))
- **(auth)** RequireMFA honors the mfaEnabled master switch (#79) ([9479754](https://github.com/orkestra-cc/orkestra/commit/94797547754a3a9271f11f9cfb938262c7bc94b3))
- **(auth)** Ship MFA, signups & OAuth providers off by default (#78) ([548b856](https://github.com/orkestra-cc/orkestra/commit/548b8562b1d9aa59d1c4ee31455172152da6b5c9))

## [0.3.1] - 2026-06-02

### Bug fixes

- **(auth)** Honor COOKIE_NAME_REFRESH, drop legacy cookie fallbacks (#77) ([ae9ae01](https://github.com/orkestra-cc/orkestra/commit/ae9ae0167acda2b56141fe82664b0b5c00465922))
- **(module)** Filter orphan module_configs docs from admin listing (#76) ([5bc5e63](https://github.com/orkestra-cc/orkestra/commit/5bc5e63ab079837abdff9609a62202293440bef9))

### Documentation

- Correct the archived-addon claim (agents/marketing have no repo) ([60960ac](https://github.com/orkestra-cc/orkestra/commit/60960ac422c34c030e39aad1f205ff2055331ef4))

## [0.3.0] - 2026-06-02

### ⚠️ Breaking Changes

- **(ci,docs)** ADR-0006 Phase 6c (part 1) — unblock CI + refresh root docs ([c21c775](https://github.com/orkestra-cc/orkestra/commit/c21c7757fe06bc459e4573bd9db20eefc3e64e8a))
- **(scripts)** ADR-0006 Phase 6b — strip runtime-profile machinery ([0c7f0f5](https://github.com/orkestra-cc/orkestra/commit/0c7f0f57b88e625db041616451af250b97af3899))
- **(docker)** ADR-0006 Phase 6a — collapse compose to one app file per env ([7cc0d3e](https://github.com/orkestra-cc/orkestra/commit/7cc0d3e883d5025266f1773adf988b49fb597982))
- **(frontend-client)** ADR-0006 Phase 5 — strip subscribe/payments surface ([1df40cd](https://github.com/orkestra-cc/orkestra/commit/1df40cdad0d375e162fa95abaf398f914d6b5a14))
- **(frontend-admin)** ADR-0006 Phase 4 — delete addon UI surface ([5f4fd34](https://github.com/orkestra-cc/orkestra/commit/5f4fd34218db779b601b4c92383a5071e67b3b9c))
- **(backend)** ADR-0006 Phase 3 — drop compliance injection probe in main.go ([d7f6d8b](https://github.com/orkestra-cc/orkestra/commit/d7f6d8b41254607c27556258c89f20838970e161))
- **(backend)** ADR-0006 Phase 2 — delete all 14 addons + AI sidecar ([c30654d](https://github.com/orkestra-cc/orkestra/commit/c30654d29dcba7650901daca3180920a6c8cca96))
- **(backend)** ADR-0006 Phase 1 — fold SDK + addons into one Go module ([23be130](https://github.com/orkestra-cc/orkestra/commit/23be1306bca41633002622bd99d295fa93321954))

### Features

- **(backend)** Re-provide core /dev/token after ADR-0006 removed the dev addon ([75e1cfb](https://github.com/orkestra-cc/orkestra/commit/75e1cfb7760f4389157d44136a47323bdd0137ae))

### Refactor

- **(backup)** Drop memgraph from backup/restore tooling (ADR-0006) ([d46f2f3](https://github.com/orkestra-cc/orkestra/commit/d46f2f306208bf056412871d848776407f0bb843))
- **(config)** Drop dead addon config structs (ADR-0006 6c leftover) ([0a49ccf](https://github.com/orkestra-cc/orkestra/commit/0a49ccfbf7335e52c352e98b698ea141b8ffe33b))

### Documentation

- **(skills)** Refresh orkestra-go skill for the core-only base (ADR-0006) ([18409cf](https://github.com/orkestra-cc/orkestra/commit/18409cf428ec23d3d5b7176ce039c17a18cb1d52))
- **(site)** Core-only drift pass — purge SKU/sidecar/addon staleness (ADR-0006) ([a9ece11](https://github.com/orkestra-cc/orkestra/commit/a9ece11c9daf29a0becef1a974be587851482c1d))
- **(site)** Remove the AI-sidecar-split page (deleted by ADR-0006) ([1b10d28](https://github.com/orkestra-cc/orkestra/commit/1b10d2861eca2f92b109d8149a7bb85ac8a65394))
- **(roadmap)** Refresh for the core-only base (ADR-0006) ([4349618](https://github.com/orkestra-cc/orkestra/commit/4349618346bdf815883414c13a16d4e2f884ad72))
- **(onboarding)** ADR-0006 Phase 6c (part 7) — banner on the SDK-split onboarding doc ([a9b741f](https://github.com/orkestra-cc/orkestra/commit/a9b741f8d82256e471c1c95096d736c7b06b7636))
- **(docker)** ADR-0006 Phase 6c (part 6) — refresh docker/CLAUDE.md ([d67977e](https://github.com/orkestra-cc/orkestra/commit/d67977e88da5b14a7adb0e3708b2dca29db85670))
- **(frontend)** ADR-0006 Phase 6c (part 5) — refresh frontend CLAUDE.md ([1150ea4](https://github.com/orkestra-cc/orkestra/commit/1150ea49e1d5e8282cfecca935b763fcb5e0b0cd))
- ADR-0006 Phase 6c (part 4) — refresh README + onboarding/site docs ([383dfac](https://github.com/orkestra-cc/orkestra/commit/383dfac219470fd7b4aa92ea99ce7f5b9791a966))
- **(site)** ADR-0006 Phase 6c (part 3) — rewrite addon-authoring pages ([8faec60](https://github.com/orkestra-cc/orkestra/commit/8faec6087adcd29b7f08021b9e040680b12e83a9))
- **(backend)** ADR-0006 Phase 6c (part 2) — refresh backend + SDK CLAUDE.md ([636a234](https://github.com/orkestra-cc/orkestra/commit/636a234d7026f7221bef85e65fc0d9d459d471a6))
- ADR-0006 collapse Orkestra to a core-only base ([75b5466](https://github.com/orkestra-cc/orkestra/commit/75b5466b97c9952ecec97483ce36a4828f5b0f8c))

### CI

- **(audit)** Scope frontend-admin npm audit to runtime deps ([076631f](https://github.com/orkestra-cc/orkestra/commit/076631f04e0c89977ec1b709bc157a3447179238))

## [0.2.2] - 2026-05-29

### Bug fixes

- **(release)** Path-unique temp names so version sync stops clobbering frontend-admin ([5a5fbf0](https://github.com/orkestra-cc/orkestra/commit/5a5fbf0dccbdf938a6b35a4bf97f1893573c46f1))
- **(frontend-admin)** Restore package.json clobbered by v0.2.1 release ([57db83b](https://github.com/orkestra-cc/orkestra/commit/57db83b48a238082e4f1e3b050e78d7d8bddfbc0))

### CI

- Publish versioned image tags on release-tag pushes ([1a6b897](https://github.com/orkestra-cc/orkestra/commit/1a6b89711e799b05dae124b59118085d0faffd41))

## [0.2.1] - 2026-05-29

### ⚠️ Breaking Changes

- **(release)** Promote dev to main (v0.2.0 follow-ups — package.json restore + cliff breaking-changes) ([2dccfe0](https://github.com/orkestra-cc/orkestra/commit/2dccfe00f06c9d36523a501049a4305ff148fbaa))
- **(release)** Add breaking-changes section to cliff.toml + regenerate CHANGELOG ([42ecb24](https://github.com/orkestra-cc/orkestra/commit/42ecb24d7cdcbfe55e7cd66f36d0a39f7e6a8d07))

### Features

- **(build)** Hot-reload runtime profiles in dev, dedupe dev compose ([1c350ba](https://github.com/orkestra-cc/orkestra/commit/1c350ba7118381ae60783b0ffb45d077e5007a18))
- **(setup)** Drop SMTP step from first-install wizard ([4582c96](https://github.com/orkestra-cc/orkestra/commit/4582c96aa55cbef1d761f4b3bdd2379fc3178f0f))

### Bug fixes

- **(jwt-keys)** Make dev JWT keys readable across container UIDs ([414d592](https://github.com/orkestra-cc/orkestra/commit/414d5926eda3875e8988fc4faadf4314838d52a0))
- Unblock first-install on a clean fork ([c5f5541](https://github.com/orkestra-cc/orkestra/commit/c5f554150ccfc41a9d3f57f1a70073aec46aa39d))
- **(release)** Restore frontend-admin/package.json clobbered by v0.2.0 release workflow ([ccfb502](https://github.com/orkestra-cc/orkestra/commit/ccfb50241bad08e547e8f4234c44ab331209043e))

### Style

- **(frontend-admin)** Round MFA banner corners and add bottom margin ([c279664](https://github.com/orkestra-cc/orkestra/commit/c279664dca42e8665cfa880f4e0dcbe8fb86846a))

### Dependencies

- **(deps)** Bump go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp (#62) ([fe7452c](https://github.com/orkestra-cc/orkestra/commit/fe7452c0b85ecccc3d88210114d425b65c9e79f0))
- **(deps)** Bump go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp (#61) ([457fdb3](https://github.com/orkestra-cc/orkestra/commit/457fdb31e0b5526ec07b47a13d0e67597c85b013))
- **(deps)** Bump go.opentelemetry.io/otel/sdk/log in /backend (#59) ([dd7f525](https://github.com/orkestra-cc/orkestra/commit/dd7f525f7d2b3b7909c179b83b410c3e3e27d458))
- **(deps)** Bump docker/setup-buildx-action from 3 to 4 (#57) ([fcf3b66](https://github.com/orkestra-cc/orkestra/commit/fcf3b66fc3b2631f66ad233045c85b38d8387692))
- **(deps)** Bump docker/build-push-action from 6 to 7 (#58) ([7bf4083](https://github.com/orkestra-cc/orkestra/commit/7bf4083e45e7bb775314e920c1811c3ba37ab131))
- **(deps)** Bump video.js from 8.23.4 to 8.23.7 in /frontend-admin (#63) ([214d22d](https://github.com/orkestra-cc/orkestra/commit/214d22da34f07a661133c82d201b0dd0717bd71e))
- **(deps)** Bump bowser from 2.13.1 to 2.14.1 in /frontend-admin (#65) ([2d08228](https://github.com/orkestra-cc/orkestra/commit/2d08228c48da806044661f518bf7df30260250c8))
- **(deps)** Bump @fullcalendar/daygrid in /frontend-admin (#66) ([fb61e71](https://github.com/orkestra-cc/orkestra/commit/fb61e7162f63dde20124e8e190d1501d1d0df2ea))
- **(deps)** Bump @hookform/resolvers in /frontend-admin (#69) ([353aab5](https://github.com/orkestra-cc/orkestra/commit/353aab505ebc9ecd567e1904cff50b5267bfc67e))

### Chores

- Normalize trailing newlines (coverage badge, generate-jwt-keys.sh) ([dab559e](https://github.com/orkestra-cc/orkestra/commit/dab559ee546213b2f4ae78bee01873d759692da0))
- Back-merge origin/main into dev (dependency bumps + CI action upgrades) ([ddacc6e](https://github.com/orkestra-cc/orkestra/commit/ddacc6e938bd86344a9107ab4d6ba524cbb93b82))
- **(frontend-client)** Sync package-lock.json to v0.2.0 ([d9431f6](https://github.com/orkestra-cc/orkestra/commit/d9431f6d306e146fb7a52a74e2ce7b569826f181))

## [0.2.0] - 2026-05-27

### ⚠️ Breaking Changes

- **(build)** Collapse SKU matrix into runtime minimal/full profiles ([f6e2aa9](https://github.com/orkestra-cc/orkestra/commit/f6e2aa90e39b7904e1037fc1d967363c77cf2ae2))

### Bug fixes

- **(versioning)** Expose APP_VERSION via VITE_APP_VERSION env var ([f37eff5](https://github.com/orkestra-cc/orkestra/commit/f37eff5ebe2943722f51ac293bfe5c1ccad6b574))
- **(release)** Reset working tree before checkout main ([0d43645](https://github.com/orkestra-cc/orkestra/commit/0d436459f0e3ccc565b5865ab176f36add535599))

## [0.1.5] - 2026-05-27

### Features

- **(navigation)** Operator reorder UI with persisted overrides ([5e58dee](https://github.com/orkestra-cc/orkestra/commit/5e58deebdd51f15dfd51dc230ba7c3957f113cbf))
- **(backup)** Add bundled backup.sh + restore.sh with dry-run and TUI ([712ab03](https://github.com/orkestra-cc/orkestra/commit/712ab03584b3e8379209fe9feb3d5b6d356254d8))

### Bug fixes

- **(orkestra-sh)** Reclaim orphan containers before `docker compose up -d` ([a07ac07](https://github.com/orkestra-cc/orkestra/commit/a07ac071d4f210a7d32e4cb892ca4c2a7239edad))
- **(orkestra-sh)** Align deploy-scope menu labels with case branches ([a514a8c](https://github.com/orkestra-cc/orkestra/commit/a514a8ccfdf3912ecc701bdcfe416c7ca9f11e7f))
- **(versioning)** Wire ORKESTRA_VERSION through compose + Dockerfiles + CI ([f866252](https://github.com/orkestra-cc/orkestra/commit/f866252b085609ae135414cf42ab51837dbac6e9))
- **(versioning)** Guard against missing __APP_VERSION__ define ([5cb5db0](https://github.com/orkestra-cc/orkestra/commit/5cb5db05b685d4c07b73c0d1c70f475b0c0e6a69))
- **(versioning)** Derive version from git tag across backend + frontends ([6c04d7f](https://github.com/orkestra-cc/orkestra/commit/6c04d7f72314750b930aab33ddf7dcd11b0c8994))

### Chores

- **(frontend-admin)** Point footer link at orkestra.cc ([a3c256d](https://github.com/orkestra-cc/orkestra/commit/a3c256d00a1c01d02a0a31d9d4e2cff69fb37ee9))
- **(orkestra-sh)** TUI header reads ORKESTRA_VERSION, drop hardcoded "1.0.0" ([01065af](https://github.com/orkestra-cc/orkestra/commit/01065afe275822cc87f83a5f4188cf5d94281f2f))
- **(orkestra-sh)** Drop "ADR-0005 Phase D" from operator-facing text ([3455d69](https://github.com/orkestra-cc/orkestra/commit/3455d691c9213303954a29efd5ece7b84eed881a))
- **(frontend-admin)** Sync package-lock.json version with package.json ([536770c](https://github.com/orkestra-cc/orkestra/commit/536770cf9053df1cc1d7e3d60c9f636316dc1696))

## [0.1.4] - 2026-05-26

### Features

- **(user,auth,compliance)** Security hardening on /admin/users (PR 6) ([2a91160](https://github.com/orkestra-cc/orkestra/commit/2a9116077c6fe7d8dc9078b794ad64e96d9b7a28))
- **(user,compliance)** Emit lifecycle audit events for admin user actions ([16195fe](https://github.com/orkestra-cc/orkestra/commit/16195fedf0594769ce05c6390ae577993bdfe603))
- **(frontend-admin/users)** Row + bulk destructive actions on /admin/users ([2fbb60e](https://github.com/orkestra-cc/orkestra/commit/2fbb60e36cadf65c6320c8bfe047d7a30cbac41b))
- **(user)** Guard destructive operator actions on /admin/users ([60493da](https://github.com/orkestra-cc/orkestra/commit/60493dac6d4757180d51c127045b77ca587594ec))
- **(user)** Real profile editing on /user/settings ([118f1d9](https://github.com/orkestra-cc/orkestra/commit/118f1d90b687bccd571c6e4e80aa59d05583bb73))

### Bug fixes

- **(auth)** Make GET /v1/auth/session read-only, end refresh race (#55) ([f2c97d9](https://github.com/orkestra-cc/orkestra/commit/f2c97d96463c4676037f4744d34fb86c7a36c99f))
- **(auth)** Pick valid refresh cookie before mutating, clear stale parent domains (#54) ([5904cfa](https://github.com/orkestra-cc/orkestra/commit/5904cfafb2660e8b07606c7a3a4603c4e8f498c2))
- **(auth,user)** Trust IdP email_verified claim on OAuth signup ([c346509](https://github.com/orkestra-cc/orkestra/commit/c34650912b351983724cba765be5455cc3951633))
- **(auth)** Skip intermediate OAuth callback page, land on profile ([232844b](https://github.com/orkestra-cc/orkestra/commit/232844bbf5386fe9caa391925bd3cc90643a954c))

### Documentation

- **(contributing)** Add fork-and-manage-addons guides ([9e192e6](https://github.com/orkestra-cc/orkestra/commit/9e192e6b9775aed713cb3957a727fb2588477864))

## [0.1.3] - 2026-05-25

### Bug fixes

- **(auth)** SocialLoginForm respects /admin/modules/auth provider toggles ([30cf831](https://github.com/orkestra-cc/orkestra/commit/30cf831148034fe34ae1b2066938b508cff70656))
- **(user)** Wire user.avatar.self permission to the avatar routes ([3540c59](https://github.com/orkestra-cc/orkestra/commit/3540c59758e1bc2b8141c081ae266e5e2768b790))

### CI

- Re-trigger backend CI after sync merge ([ea36c11](https://github.com/orkestra-cc/orkestra/commit/ea36c11a1760b5784f56c028cca6172ea2b16861))

### Chores

- **(sync)** Merge main into dev (avatar pipeline + release hygiene) ([864a634](https://github.com/orkestra-cc/orkestra/commit/864a634d94aa6f62f275b3dbaf6612f6c3ae804f))

## [0.1.2] - 2026-05-24

### Features

- **(frontend-client)** Self-service avatar surface ([92f6d2f](https://github.com/orkestra-cc/orkestra/commit/92f6d2f4ef74f21d88ae1d516c401ada3968122a))
- **(avatar)** Self-service user avatar pipeline (upload / OAuth / initials) ([e800fc0](https://github.com/orkestra-cc/orkestra/commit/e800fc09121a120df8da37ba8ca1305bacf7f5e4))
- **(auth)** MfaMethods allow-list gates factor enrollment ([bd10251](https://github.com/orkestra-cc/orkestra/commit/bd102513a87e4d6a2c84eab2eacacf7446f3909e))
- **(auth)** Live admin reads for accessTokenTTL + passwordResetTokenTTL ([e232595](https://github.com/orkestra-cc/orkestra/commit/e23259507d13318efc46f8d2f9101e89b44a72ff))
- **(auth)** Persistent audit log for security events ([ff5728a](https://github.com/orkestra-cc/orkestra/commit/ff5728af9887d3296d6241d83c95cbe742baa09c))

### Bug fixes

- **(auth)** Silence tenantscope on auth_security_events repo ([3e8afb2](https://github.com/orkestra-cc/orkestra/commit/3e8afb22730158c09cbb91a3110be0bede43206f))

### Refactor

- **(auth)** Close all open TODOs in core/auth ([50062fe](https://github.com/orkestra-cc/orkestra/commit/50062fe6266dd107352f810f0c2b6074f6f2725c))

### Tests

- **(core)** Expand safety-net coverage on navigation, logging, auth ([4daead4](https://github.com/orkestra-cc/orkestra/commit/4daead4b5075c9bfb397dfbece194a04bf246c2f))

### CI

- **(release)** Bump softprops/action-gh-release to v3 for Node 24 runtime ([0ec8171](https://github.com/orkestra-cc/orkestra/commit/0ec817174288e8466fca44915606426aa78f373b))

### Chores

- **(openapi)** Regenerate enterprise.json after PR #52 routes ([8d0af99](https://github.com/orkestra-cc/orkestra/commit/8d0af997f6c199887e762005221b695a7f6aebf0))
- **(sync)** Merge main into dev (Dependabot bumps #39 #48 #49 + badge refresh) ([d0a9a1a](https://github.com/orkestra-cc/orkestra/commit/d0a9a1aa85a4b16e7e707f430e30b83bcd01737b))

## [0.1.1] - 2026-05-24

### Bug fixes

- **(release)** Defensive guard when CHANGELOG.md is already current ([d9d9b5f](https://github.com/orkestra-cc/orkestra/commit/d9d9b5fc757040bee67da5a19d7f4d72f6f2e632))

### Dependencies

- **(deps)** Bump actions/upload-artifact from 5 to 7 (#49) ([df49327](https://github.com/orkestra-cc/orkestra/commit/df49327bb31cbfcdd896cf34c433171bead4d22a))
- **(deps)** Bump actions/checkout from 5 to 6 (#48) ([aafed72](https://github.com/orkestra-cc/orkestra/commit/aafed723d9a01b7260f7f627625f038d082d0026))
- **(deps)** Bump github.com/cedar-policy/cedar-go in /backend (#39) ([105016a](https://github.com/orkestra-cc/orkestra/commit/105016a418e7b0396ea6101d57cbc6dd034d3061))
- **(deps)** Bump actions/download-artifact from 5 to 8 (#35) ([7a35304](https://github.com/orkestra-cc/orkestra/commit/7a35304270995d6bad7f0ba7f2ba370243b62cb0))
- **(deps)** Bump cupertino_icons from 1.0.8 to 1.0.9 in /mobile (#47) ([95be287](https://github.com/orkestra-cc/orkestra/commit/95be28787a2e565752f27975667cb3cb233b8639))
- **(deps)** Bump emoji-picker-react in /frontend-admin (#44) ([eb12a71](https://github.com/orkestra-cc/orkestra/commit/eb12a7118f1bb70127379cc0df26d610d9b90d37))
- **(deps)** Bump react-hook-form in /frontend-admin (#41) ([2776e67](https://github.com/orkestra-cc/orkestra/commit/2776e67fc1c14bae7e988ca60223e23309d1b25a))
- **(deps)** Bump web-vitals from 5.1.0 to 5.2.0 in /frontend-admin (#40) ([1ea970a](https://github.com/orkestra-cc/orkestra/commit/1ea970aedb4e7de04b18274773bb8f6493abbec4))
- **(deps)** Bump github.com/go-webauthn/webauthn in /backend (#38) ([1f06f49](https://github.com/orkestra-cc/orkestra/commit/1f06f49b70f24f08762c3971a84328cc76a4a78b))
- **(deps)** Bump github.com/redis/go-redis/v9 in /backend (#37) ([7c3ee11](https://github.com/orkestra-cc/orkestra/commit/7c3ee114e94732ce9ee95f6fcb71ef0e90f658c8))
- **(deps)** Bump github.com/alicebob/miniredis/v2 in /backend (#36) ([6c81285](https://github.com/orkestra-cc/orkestra/commit/6c81285bb7de29147494d034d7f73b8d98a12d99))
- **(deps)** Bump golang.org/x/tools from 0.44.0 to 0.45.0 in /backend (#34) ([ab9d3c1](https://github.com/orkestra-cc/orkestra/commit/ab9d3c1a78db74a03ea4c05672941b40f7ffbb04))

## [0.1.0] - 2026-05-23

### Features

- **(mobile)** Dart-define overrides for fork-friendly backend URLs (fork-readiness Phase 5) ([6461b45](https://github.com/orkestra-cc/orkestra/commit/6461b45ff001b17aba3b3636dca5885430ffbd48))
- **(docker)** Dev-public compose with public Alpine images (fork-readiness Phase 3) ([6aa1ac8](https://github.com/orkestra-cc/orkestra/commit/6aa1ac83d92d8f9f257c2e616a3d7b525cee7f53))
- **(init)** Bootstrap script for fork-friendly first-boot setup ([7ceaeb0](https://github.com/orkestra-cc/orkestra/commit/7ceaeb0cb0e949634b1d6ea13db267035cb8d0fa))
- **(marketing)** Show Gravatar avatars in contacts tables ([be695d2](https://github.com/orkestra-cc/orkestra/commit/be695d2bae97c2faea04100070463569a20ff215))
- **(marketing)** Adopt AdvanceTable + CSV export for marketing tables ([d1bf117](https://github.com/orkestra-cc/orkestra/commit/d1bf1176e89c4bf1d0c15c5f43f8d1ecbb05e10a))
- **(marketing)** Phase 4 frontend + docs (PR-5) ([16d0e17](https://github.com/orkestra-cc/orkestra/commit/16d0e1773abb5054cf8efcbef34e9a6c39d46747))
- **(marketing)** Phase 4 Phase-3-leftovers backend (PR-4) ([8c631f9](https://github.com/orkestra-cc/orkestra/commit/8c631f9da089679345e9a7a19f02b18c53dd5dfe))
- **(marketing)** Phase 4 HTTP + Cedar + persons-list filters (PR-3) ([ee88510](https://github.com/orkestra-cc/orkestra/commit/ee88510d6a9b22bcc32bf843ad0fec72842c6184))
- **(marketing)** Phase 4 card lifecycle services + scheduler (PR-2) ([fa8006b](https://github.com/orkestra-cc/orkestra/commit/fa8006b927e31765c4154dccaf831e6232b5ff42))
- **(marketing)** Phase 4 data layer + code-format generator (PR-1) ([67fe414](https://github.com/orkestra-cc/orkestra/commit/67fe41406469eeb5b78d9b20f8ce46863310a353))
- **(marketing)** Phase 3 admin UI — Reviews queue, adapter picker, async wizard (PR-5) ([8fe965b](https://github.com/orkestra-cc/orkestra/commit/8fe965b6c7f58b3283205ad368ef212c7626d183))
- **(marketing)** Phase 3 Odoo JSON-2 adapter + engagement-CSV detection (PR-4) ([65c8577](https://github.com/orkestra-cc/orkestra/commit/65c8577b901d76e95adc98a9fd7d2be5bd2b6873))
- **(marketing)** Phase 3 Excel adapter (PR-3) ([16c00be](https://github.com/orkestra-cc/orkestra/commit/16c00bebd772ca1661f2755d205032ba49bc6f13))
- **(marketing)** Phase 3 conflict-review queue + soft-match + auto-emission (PR-2) ([78b8e44](https://github.com/orkestra-cc/orkestra/commit/78b8e442c7c015fb644dfc9a256ed80d077d6bbb))
- **(marketing)** Phase 3 data layer + async runner (PR-1) ([0ceeb4d](https://github.com/orkestra-cc/orkestra/commit/0ceeb4de7caca3f92fa5683c6ae68b68d9f6aca7))
- **(marketing)** Phase 2 admin UI — Timeline, Scores, /marketing/scoring (PR-5) ([8f95c71](https://github.com/orkestra-cc/orkestra/commit/8f95c7194e838f130846c458a8e4606cc03c661a))
- **(marketing)** Phase 2 HTTP surface — activities, profiles, snapshots (PR-4) ([b61fea1](https://github.com/orkestra-cc/orkestra/commit/b61fea158c9ee9de3b1fcf3fb9832e6a88857706))
- **(marketing)** Phase 2 scheduler — eager + nightly recompute (PR-3) ([32108fe](https://github.com/orkestra-cc/orkestra/commit/32108fe7031ccb5823a4e8207f1b39a61f248089))
- **(marketing)** Phase 2 pure score engine — scoring/ package (PR-2) ([e817491](https://github.com/orkestra-cc/orkestra/commit/e81749151b254a247a4e55d9b96f44165b788610))
- **(marketing)** Phase 2 data layer — activities, score profiles, snapshots (PR-1) ([37c1fe7](https://github.com/orkestra-cc/orkestra/commit/37c1fe732a818fccf01dab9b8fcc479e1ed345b8))
- **(frontend-admin)** I18n sweep for marketing tags/custom-fields/contacts list ([65520cc](https://github.com/orkestra-cc/orkestra/commit/65520cc54a5738f97b6e8504221cc1d3fd32ba4f))
- **(frontend-admin)** Translate sidebar nav labels (Phase 6 follow-up) ([f296d2d](https://github.com/orkestra-cc/orkestra/commit/f296d2de74c337b2f2cd3d68a55fadd10dcbc30e))
- **(frontend-admin)** Italian completion pass (Phase 6) ([47ac08e](https://github.com/orkestra-cc/orkestra/commit/47ac08e1f2993850d0ede5ccf301d5dce8cf8ed5))
- **(frontend-admin)** Add language picker to user preferences (Phase 5) ([cd4d31b](https://github.com/orkestra-cc/orkestra/commit/cd4d31be568a40d941297c53066efd257eb22cd3))
- **(frontend-admin)** Wire final user/profile/Banner scaffold (Phase 4 long tail) ([8749901](https://github.com/orkestra-cc/orkestra/commit/8749901eb50a6fc110ec013435f5dd2b93fc1719))
- **(frontend-admin)** Extract User Profile + Settings scaffold (Phase 4 long tail) ([21fefd5](https://github.com/orkestra-cc/orkestra/commit/21fefd5050af92e0c1f38c4fcfcd7e52f74ec0d9))
- **(frontend-admin)** Extract User scaffold pages (Dashboard + Calendar + ProfileSettings) (Phase 4 long tail) ([aa49c87](https://github.com/orkestra-cc/orkestra/commit/aa49c871300c86a500ac2bed1c2b34d53484cc98))
- **(frontend-admin)** Extract User Privacy GDPR pages (Phase 4 long tail) ([70b3b5f](https://github.com/orkestra-cc/orkestra/commit/70b3b5f8195b5df54a4ee6954130414125c6ef5d))
- **(frontend-admin)** Extract User Security LinkedProvidersTab (Phase 4 long tail) ([e937c14](https://github.com/orkestra-cc/orkestra/commit/e937c148c0cfaae00ffbce9fb362c18bbfa854a2))
- **(frontend-admin)** Extract User Security SessionsTab + TrustedDevicesTab (Phase 4 long tail) ([47e5668](https://github.com/orkestra-cc/orkestra/commit/47e5668cb935dc750a5031f6c5af039a284e2240))
- **(frontend-admin)** Extract User Security BackupCodesTab + BackupCodesDisplay (Phase 4 long tail) ([c0ad4d3](https://github.com/orkestra-cc/orkestra/commit/c0ad4d327a5f4cd6e583a474274f6838ed3626cd))
- **(frontend-admin)** Extract User Security page chrome + PasswordTab (Phase 4 long tail) ([38e097b](https://github.com/orkestra-cc/orkestra/commit/38e097b1c6b705408307dc2cd1522658515f9c87))
- **(frontend-admin)** Extract Billing companies page + greetings (Phase 4 long tail) ([23e2b5f](https://github.com/orkestra-cc/orkestra/commit/23e2b5fc79cebd20a805ea05287e690bb54f56ef))
- **(frontend-admin)** Extract PersonalAgentChat (Phase 4 long tail) ([553a474](https://github.com/orkestra-cc/orkestra/commit/553a474aac436b9b104e4d6509eb3023ea99e9d6))
- **(frontend-admin)** Extract Marketing Contacts detail (Phase 4 long tail) ([4d2e8e4](https://github.com/orkestra-cc/orkestra/commit/4d2e8e456d76f22cbc007a52fe631ac322d727ef))
- **(frontend-admin)** Extract Company Search filters + results (Phase 4 long tail) ([dab679c](https://github.com/orkestra-cc/orkestra/commit/dab679cc18b41dcb3b6e6dacdd92b6e11c71e7f6))
- **(frontend-admin)** Extract CompanyEnrichment (Phase 4 long tail) ([2ff375c](https://github.com/orkestra-cc/orkestra/commit/2ff375c1aad189b149d3b5708d6a4cf06986d84b))
- **(frontend-admin)** Extract Company Lookup pages (Phase 4 long tail) ([d3c2d3b](https://github.com/orkestra-cc/orkestra/commit/d3c2d3b04f94c642fbc9d8bb9dafc0e9289b5a20))
- **(frontend-admin)** Extract Setup wizard (Phase 4 long tail) ([0a649d9](https://github.com/orkestra-cc/orkestra/commit/0a649d95a0c3fd98f91ceddf347df4d1f2c7cbb5))
- **(frontend-admin)** Extract AdminUserActions (Phase 4 long tail) ([9ad660c](https://github.com/orkestra-cc/orkestra/commit/9ad660cef84a99b8b5690addc480b344bfa75f56))
- **(frontend-admin)** Extract AdminAuthMethodsCard (Phase 4 long tail) ([8874529](https://github.com/orkestra-cc/orkestra/commit/8874529be3a18e3fc569cafe615e9645c1d93a66))
- **(frontend-admin)** Rename operatorProfile → profileShared + extract Admin User Profile banner/intro/metrics (Phase 4 long tail) ([8460d86](https://github.com/orkestra-cc/orkestra/commit/8460d86c9b81eb844fb0430c874db22397f6f7bb))
- **(frontend-admin)** Extract Internal Tenants detail + Operator Profile (Phase 4 long tail) ([648bb91](https://github.com/orkestra-cc/orkestra/commit/648bb91b233ebf6f3c9479e69578e1d88490b969))
- **(frontend-admin)** Extract Admin Roles tables + PermissionPicker (Phase 4 long tail) ([e277c59](https://github.com/orkestra-cc/orkestra/commit/e277c59e53d218247a3f2a26c1ebb97c310beafe))
- **(frontend-admin)** Extract Admin Audit Events Filters + Table (Phase 4 long tail) ([b957db6](https://github.com/orkestra-cc/orkestra/commit/b957db6493a3ba9fffc48cf62e334c6cdcaa6ed1))
- **(frontend-admin)** Extract Admin Tenants TenantTable + TenantTableHeader (Phase 4 long tail) ([75a863a](https://github.com/orkestra-cc/orkestra/commit/75a863af61fec5412420bb936402cca69ba14d26))
- **(frontend-admin)** Extract Admin Users CreateUser modal + table header (Phase 4 long tail) ([4776b90](https://github.com/orkestra-cc/orkestra/commit/4776b9047c63029b6522116e0d75d697695f6942))
- **(frontend-admin)** Extract Clients BillingIdentity tab (Phase 4 long tail) ([18b77af](https://github.com/orkestra-cc/orkestra/commit/18b77af395bddd1697cd8de2e3fd6a672060b110))
- **(frontend-admin)** Extract Clients Divisions + Subscriptions + Payments + Activity (Phase 4 long tail) ([9201e95](https://github.com/orkestra-cc/orkestra/commit/9201e95028460679aac5102cdb108dfef0372c0f))
- **(frontend-admin)** Extract Clients Members tab + AttachMember modal (Phase 4 long tail) ([ba225df](https://github.com/orkestra-cc/orkestra/commit/ba225df7f2a1d5900383b552c16f2916ebadf540))
- **(frontend-admin)** Extract Clients detail chrome + Overview + Impersonate (Phase 4 long tail) ([b49509b](https://github.com/orkestra-cc/orkestra/commit/b49509b1117cff56c8c2cb807a64fff4d1a5cbea))
- **(frontend-admin)** Extract Graph explorer + components (Phase 4 long tail) ([149cd9c](https://github.com/orkestra-cc/orkestra/commit/149cd9c700c4a8cad907aa9f0e42beca25c8d3cd))
- **(frontend-admin)** Extract Graph documents + rag pages (Phase 4 long tail) ([6223d47](https://github.com/orkestra-cc/orkestra/commit/6223d476ff1c4faee83368582c7516d1d3e921a7))
- **(frontend-admin)** Extract Graph relationships + vector pages (Phase 4 long tail) ([4c059b0](https://github.com/orkestra-cc/orkestra/commit/4c059b0f1ed60828418199b723e55c41e6926a21))
- **(frontend-admin)** Extract Graph databases + algorithms pages (Phase 4 long tail) ([0d91404](https://github.com/orkestra-cc/orkestra/commit/0d914041fa2d9a146dbc3584d29d1cb8262da923))
- **(frontend-admin)** Extract AI Agents chat bundle (Phase 4 long tail) ([932607b](https://github.com/orkestra-cc/orkestra/commit/932607b791dee3f8219c4926b20d203f0c951b88))
- **(frontend-admin)** Extract marketing imports + wizard (Phase 4 long tail) ([1669775](https://github.com/orkestra-cc/orkestra/commit/16697753a119327fe5cef0f44e380ed4ce067458))
- **(frontend-admin)** Wire Compliance SOC2 evidence page (Phase 4 long tail) ([0cbd5f4](https://github.com/orkestra-cc/orkestra/commit/0cbd5f48c4c7f57a0407704dac305840d634251e))
- **(frontend-admin)** Extract AI Models admin bundle (Phase 4 long tail) ([90f3ed0](https://github.com/orkestra-cc/orkestra/commit/90f3ed03f0d0a038485ce22949ca786a125fcef6))
- **(frontend-admin)** Extract Documents Templates bundle (Phase 4 long tail) ([d0388c8](https://github.com/orkestra-cc/orkestra/commit/d0388c887e2baf15bcdd80326de820f5cfe7f448))
- **(frontend-admin)** Extract MFA enroll wizard + settings panel (Phase 4 long tail) ([0fc0fa7](https://github.com/orkestra-cc/orkestra/commit/0fc0fa790aba3e47e2b4367abc7fef5e31dba38b))
- **(frontend-admin)** Extract module config field renderer + AI models section (Phase 4 long tail) ([a16565c](https://github.com/orkestra-cc/orkestra/commit/a16565cf7676e66a9c20d9183e7fe8328db42efe))
- **(frontend-admin)** Extract sales jobs/prospect/reports/skills (Phase 4 long tail) ([8794969](https://github.com/orkestra-cc/orkestra/commit/8794969eb1640fd3a720227dc51b4ec03ac1a881))
- **(frontend-admin)** Extract Sales Settings page (Phase 4 long tail) ([250cce2](https://github.com/orkestra-cc/orkestra/commit/250cce2544b367bebc41acfb284a88dfa7fab786))
- **(frontend-admin)** Extract admin role/binding/audit/tenant modals (Phase 4 long tail) ([383b12a](https://github.com/orkestra-cc/orkestra/commit/383b12adf3a972e918957e7e54fd09af8920724e))
- **(frontend-admin)** Extract subscriptions+payments deep forms (Phase 4 long tail) ([ab787e1](https://github.com/orkestra-cc/orkestra/commit/ab787e10c83d1615ca84cbb39497aa9ffb5e9ddd))
- **(frontend-admin)** Wire identity OIDC/SCIM forms (Phase 4 long tail) ([ec9720b](https://github.com/orkestra-cc/orkestra/commit/ec9720b8931fadb833a196a05a7fb4e8b8d287c4))
- **(frontend-admin)** Extract billing dashboard cards (Phase 4 long tail) ([7c48156](https://github.com/orkestra-cc/orkestra/commit/7c48156be618457cfd02bf41cca70449fc0f1163))
- **(frontend-admin)** Extract billing forms triple (Phase 4 long tail) ([08045f6](https://github.com/orkestra-cc/orkestra/commit/08045f6bc1df603750464e34abe39ea760cafd50))
- **(frontend-admin)** Extract CompanyModal SDI form (Phase 4 long tail) ([6198cc7](https://github.com/orkestra-cc/orkestra/commit/6198cc7fb640451d2f3f7f6961591a273e8890ea))
- **(frontend-admin)** Extract NewIssuedInvoice SDI form (Phase 4 long tail) ([afe9a4a](https://github.com/orkestra-cc/orkestra/commit/afe9a4adb7a0f4bf8fd3bcbc4e7ae5c669a7d699))
- **(frontend-admin)** Extract IssuedInvoiceDetail SDI form (Phase 4 long tail) ([51891b2](https://github.com/orkestra-cc/orkestra/commit/51891b25c3d2585c49603b1a17fdd3011edc5f22))
- **(frontend-admin)** Extract ChangePassword + identity locale keys (Phase 4 long tail) ([5fcb2b0](https://github.com/orkestra-cc/orkestra/commit/5fcb2b099ed8de7fc38f6bfb7510736ca0d76eb1))
- **(frontend-admin)** Extract MFA Remove/WebAuthn modals (Phase 4 long tail) ([b581533](https://github.com/orkestra-cc/orkestra/commit/b5815331411d270c82f476ab4ea3fcc53f01274d))
- **(frontend-admin)** Extract DeleteRoleModal (Phase 4 long tail) ([871e6b5](https://github.com/orkestra-cc/orkestra/commit/871e6b52d5a9d6ec570fd0b1a86fcdc7153691d7))
- **(frontend-admin)** Extract tenant Create/Purge modals (Phase 4 long tail) ([69e6619](https://github.com/orkestra-cc/orkestra/commit/69e6619784d1180698f126d8e74aeef9606a8953))
- **(frontend-admin)** Extract tenant DeleteModal (Phase 4 long tail) ([f3811a6](https://github.com/orkestra-cc/orkestra/commit/f3811a6b493a047b18e81fc0dc0f6df0af5a0602))
- **(frontend-admin)** Extract module detail config + cards (Phase 4 long tail) ([62772b9](https://github.com/orkestra-cc/orkestra/commit/62772b911d4f69200bd39dece4dae8b27872697e))
- **(frontend-admin)** Extract sales + audit-events chrome (Phase 4.14 + 4.15) ([92c7b63](https://github.com/orkestra-cc/orkestra/commit/92c7b630349771b257b6efb2015cd052227aba56))
- **(frontend-admin)** Extract addon page chrome — identity, marketing, compliance (Phase 4.14 partial) ([923fa7b](https://github.com/orkestra-cc/orkestra/commit/923fa7bc65d639c7716cb9f10cfc426717a6b7da))
- **(frontend-admin)** Extract subscriptions + payments chrome (Phase 4.13) ([6d82448](https://github.com/orkestra-cc/orkestra/commit/6d824487ed5ac7bdbd2456309c531d663dbb7633))
- **(frontend-admin)** Extract billing + company chrome (Phase 4.10 + 4.12) ([39b837a](https://github.com/orkestra-cc/orkestra/commit/39b837a6388f99910eee3b963f2b8db9bf1cccdc))
- **(frontend-admin)** Extract tenants/clients/roles/observability chrome (Phase 4.5-9) ([052c614](https://github.com/orkestra-cc/orkestra/commit/052c614e8a540e9e0c5a4c0b52e980e8867442ba))
- **(frontend-admin)** Extract /admin/users chrome (Phase 4.4) ([635f1db](https://github.com/orkestra-cc/orkestra/commit/635f1db4b619faec96e3f47adff0d89077c5b621))
- **(frontend-admin)** Extract /admin/modules chrome (Phase 4.3) ([20ba2d0](https://github.com/orkestra-cc/orkestra/commit/20ba2d0d9d2be2a06aa674a7b41db4f0c7ba1aa9))
- **(frontend-admin)** Extract user settings strings (Phase 4.17) ([e9b7774](https://github.com/orkestra-cc/orkestra/commit/e9b7774118fce399177715705762d4e70998036c))
- **(frontend-admin)** Extract i18n strings for auth screens (Phase 4.2) ([dc0bbdb](https://github.com/orkestra-cc/orkestra/commit/dc0bbdb0c21345ee81e6fb3b7a44991febf83077))
- **(frontend-admin)** Extract i18n strings for shared chrome (Phase 4.1) ([5e82742](https://github.com/orkestra-cc/orkestra/commit/5e8274219e448e7bf8d53706755be0fafe7c1e19))
- **(frontend-admin)** Bootstrap react-i18next (EN default + IT) ([686d357](https://github.com/orkestra-cc/orkestra/commit/686d35760a9661f66b146788dacb8011b3303d4e))
- **(errcode)** Error-code contract + AuthEmailInUse worked example ([05e1bea](https://github.com/orkestra-cc/orkestra/commit/05e1bea900b2ddbd097a3a9fe7d9b80b62abc0f6))
- **(user)** Add language preference field + PATCH /me self-service ([a3557ae](https://github.com/orkestra-cc/orkestra/commit/a3557ae19fccf50ffe0a8ce07ea8941c615f601a))
- **(frontend-admin)** Collapsible navbar realm sections ([ce3c638](https://github.com/orkestra-cc/orkestra/commit/ce3c6386204d317ffc1c6509490e57c87b5763d2))
- **(marketing)** Merge Phase 1 — contact base + CSV importer + admin UI ([52ca4b6](https://github.com/orkestra-cc/orkestra/commit/52ca4b6d547f12c4748aff64e80d0a068b65a9bb))
- **(marketing)** React admin pages, manifest, and API slice (PR-5) ([348d21b](https://github.com/orkestra-cc/orkestra/commit/348d21b93f93e4a50da30d944963af3e47a76f7e))
- **(marketing)** CSV importer with sync pipeline (PR-4) ([aaa3efe](https://github.com/orkestra-cc/orkestra/commit/aaa3efecc3bd24d71b073bfded02a8a27e2b6d16))
- **(marketing)** API layer with handlers, routes, and services (PR-3) ([f3e742a](https://github.com/orkestra-cc/orkestra/commit/f3e742a468898ce4b2542421fdc483b5322fe2ce))
- **(marketing)** Data layer for the 5 Phase-1 collections (PR-2) ([b383886](https://github.com/orkestra-cc/orkestra/commit/b383886dfbee20f54d61dd446121a5401bf65f22))
- **(marketing)** Scaffold new addon module (Phase 1 PR-1) ([2d21936](https://github.com/orkestra-cc/orkestra/commit/2d219367558fa13096821f9256e3c616ad7cd4a8))
- **(observability)** Five operator dashboards for the trifecta (ADR-0005) ([f636d69](https://github.com/orkestra-cc/orkestra/commit/f636d69a2a21cece4f68a06dd1b0dc2279134d91))
- **(observability)** Admin UI for runtime log levels (ADR-0005 Phase F) ([0b63bdf](https://github.com/orkestra-cc/orkestra/commit/0b63bdf9b976397f1c212f6dc1c0f059c0f31f24))
- **(observability)** OTLP logs exporter — Tier 2 fanout (ADR-0005 Phase E) ([dd56c64](https://github.com/orkestra-cc/orkestra/commit/dd56c64c1d00f75d2b901b057dae7e89501a5ef0))
- **(observability)** Self-hosted Loki + Promtail stack (ADR-0005 Phase D) ([2857f5b](https://github.com/orkestra-cc/orkestra/commit/2857f5baf51a915895a0a04ecc1ac9bda6e76579))
- **(observability)** Per-module log levels (ADR-0005 Phase C) ([0f5dafc](https://github.com/orkestra-cc/orkestra/commit/0f5dafc049f1ac20e40f487f880300a153b645b5))
- **(observability)** HTTP latency histogram with trace_id exemplars (ADR-0005 Phase B) ([2545c7d](https://github.com/orkestra-cc/orkestra/commit/2545c7dd782c387bd9cf884af464525039def53c))
- **(observability)** Structured request logger + trace correlation (ADR-0005 Phase A) ([d55ee6e](https://github.com/orkestra-cc/orkestra/commit/d55ee6e90255fbc7065530eaf794879c5dab5f2b))
- **(backend)** Canonical openapi/enterprise.json + ci-backend openapi-check gate ([3abb988](https://github.com/orkestra-cc/orkestra/commit/3abb988d295873da45969241d42de869e55a7ea3))
- **(frontend)** Runtime config + add frontend-admin to SKU composes ([33c5cba](https://github.com/orkestra-cc/orkestra/commit/33c5cba2b91a0059d3203a52e693868bb10e6605))
- **(module)** Add ConfigService.UnmarshalModule ([a534980](https://github.com/orkestra-cc/orkestra/commit/a534980903ece812e2da52e97e62ce0c04fe4962))
- **(frontend-admin)** Add Developer nav realm for Falcon demos ([7d023df](https://github.com/orkestra-cc/orkestra/commit/7d023df4f222d0a2a7d5d67fced96110c2113c8d))
- **(auth)** Self-service OAuth provider linking on /user/security ([032ce33](https://github.com/orkestra-cc/orkestra/commit/032ce3323f64e8bad126b1ef58328700a625e82e))
- **(auth)** Self-service /user/security page with session control ([dd4c733](https://github.com/orkestra-cc/orkestra/commit/dd4c733882801ff36bc1c71b2f3fe80ed146e1b3))
- **(auth)** Admin-managed Authentication Methods card on user profile ([55d19ac](https://github.com/orkestra-cc/orkestra/commit/55d19ac8681f796dec441f1252917d52a0e89e22))
- **(billing)** Scope dashboard cards to YTD and Last Year ([5725d5a](https://github.com/orkestra-cc/orkestra/commit/5725d5a16aa599fa77ad35a1c16c1242f7544fcf))
- **(build-profiles)** Pre-enable SKU addons via ORKESTRA_PROFILE ([fd03a80](https://github.com/orkestra-cc/orkestra/commit/fd03a80315fa4804d853ebd9a5a525bae2d55e93))
- **(orkestra-sh)** SKU profile commands for GHCR-pulled image deploys ([1ad603c](https://github.com/orkestra-cc/orkestra/commit/1ad603ccafa831ed2aa413f97a839fe9d8de95fa))
- **(docker)** Per-profile compose files for SKU image deploys ([3a30928](https://github.com/orkestra-cc/orkestra/commit/3a3092800d19c78ae697a25b907b923dcb8f0865))
- **(build-profiles)** Tag-gated addon catalog + profile build matrix ([77f430e](https://github.com/orkestra-cc/orkestra/commit/77f430e629744b1d75add67c314042475827ff58))
- **(admin-clients)** Search tenants by member email/surname ([bc3ca46](https://github.com/orkestra-cc/orkestra/commit/bc3ca4687ac75f5171eb62d92a92a3982b227b45))
- **(admin-clients)** Impersonate + member admin actions ([1d13406](https://github.com/orkestra-cc/orkestra/commit/1d13406402600097d591a711af4875abcef2b681))
- **(unified-clients)** Split impersonation audit + MFA gate (Phase 7) ([c116e56](https://github.com/orkestra-cc/orkestra/commit/c116e56330f079da94abd1d4f194b73a3044355b))
- **(unified-clients)** URL merge + self-service billing identity (Phase 6) ([ebd2e42](https://github.com/orkestra-cc/orkestra/commit/ebd2e425390ee824191910d0777aaf95ed506587))
- **(billing)** Fold Customer into Tenant.FatturaPA (Phase 5) ([da9b426](https://github.com/orkestra-cc/orkestra/commit/da9b426f971e13713cd98f22edc6ea03e8eb75d1))
- **(tenant)** Collapse Owner polymorphism to tenantUUID (Phase 4) ([695f14d](https://github.com/orkestra-cc/orkestra/commit/695f14dcb05f72ec4ee3a8ba6243dbc028c27b74))
- **(migration)** Add Phase 3 unify-clients migration + flip lazy flag default ([c8ec56b](https://github.com/orkestra-cc/orkestra/commit/c8ec56bff28bf43c08251cfdeb953ee3e73f0f65))
- **(tenant)** Add Phase 2 lazy-tenant flag wiring ([79b88e5](https://github.com/orkestra-cc/orkestra/commit/79b88e53f3f2e7c54c7baed8b94fefb95b098206))
- **(tenant)** Add Phase 1 unified-client fields + billing seam ([ff6736f](https://github.com/orkestra-cc/orkestra/commit/ff6736fec3d64ae24c05df62293748bc7e4549d2))
- **(user)** Client invite + admin auth triggers ([d92ac1f](https://github.com/orkestra-cc/orkestra/commit/d92ac1fc482d0b6a22b60b84e1027c0fc3a828e3))
- **(user)** Admin client-user CRUD + attach/detach ([1d35d08](https://github.com/orkestra-cc/orkestra/commit/1d35d082d2c7174439b83624e5945e41bb7e75b2))
- **(auth)** Tab 10-A — recovery codes count + oauth auto-link gate ([225452a](https://github.com/orkestra-cc/orkestra/commit/225452a55a0a72ca3b781465bf42df2f8630e8e5))
- **(auth)** Tab 9 small backlog — oauth signup + admin mfa role list ([d6c79e5](https://github.com/orkestra-cc/orkestra/commit/d6c79e574d089616fabb082d55d41a8d3b599c97))
- **(auth)** Tab 8 trivial toggles — sessions & account ([96ae592](https://github.com/orkestra-cc/orkestra/commit/96ae592fe43ff01fa2ebb8645e772f626d1fe897))
- **(auth)** Tab 7 anti-abuse & notifications ([70e514c](https://github.com/orkestra-cc/orkestra/commit/70e514c04fd49b431cf6023ab91971f6bf84bcf3))
- **(auth)** Public policy endpoint + SPA UX gating ([728490f](https://github.com/orkestra-cc/orkestra/commit/728490f74f5e5e804d7f5761671431b519761e6d))
- **(auth)** Admin-managed authentication policy ([14261ec](https://github.com/orkestra-cc/orkestra/commit/14261eca658e8ca36dd3c991d417909f49dd023a))
- **(frontend-client)** Owner-scoped dashboard ([0055049](https://github.com/orkestra-cc/orkestra/commit/0055049ad13306e29276c19d646a2b603a98d4af))
- **(tenant)** Admin direct-attach member endpoint ([31d54cd](https://github.com/orkestra-cc/orkestra/commit/31d54cdc4a59107a286d70bad8e00f883b5418dd))
- **(frontend-client)** Billing profile + subscribe ([62b6696](https://github.com/orkestra-cc/orkestra/commit/62b66961024c275d4cec10adcece93f0205fa2f5))
- **(clientbilling)** User-level billing profile addon ([f5475a1](https://github.com/orkestra-cc/orkestra/commit/f5475a1c8f9549386d1913b142625895521b9c9b))
- **(tenant)** Cascade-delete authz bindings and orphan owner ([d4f8b96](https://github.com/orkestra-cc/orkestra/commit/d4f8b96f57018cbc20e7fc5b7c96d0729e6fb2f3))
- **(auth)** Self-service verification email resend ([e8154dd](https://github.com/orkestra-cc/orkestra/commit/e8154ddc5d44d8e57500ee4bac79d2c803cd58fa))
- **(tenant)** Show member email in admin members list ([c7071c5](https://github.com/orkestra-cc/orkestra/commit/c7071c5e2761623d4c05b96c01c765ec1eb99d73))
- **(auth)** Per-tier frontend URL for verification email ([26c860c](https://github.com/orkestra-cc/orkestra/commit/26c860cf5ae178ec262b8892abb6c1d0c1f3645b))
- **(frontend-client)** Self-subscribe + Stripe Checkout ([f52fede](https://github.com/orkestra-cc/orkestra/commit/f52fedefbf927dddf1ff9eb1951f9d4c55e9fabe))
- **(frontend-client)** Login + account + MFA enrolment ([fc59940](https://github.com/orkestra-cc/orkestra/commit/fc59940a31a911159963ded257f38ef9232df676))
- **(frontend-client)** Catalog browse + signup + verify-email ([383a544](https://github.com/orkestra-cc/orkestra/commit/383a5441f57a05b0394de15c004c5b7ee242bf6f))
- **(frontend-client)** Scaffold Tier-2 demo SPA ([134546e](https://github.com/orkestra-cc/orkestra/commit/134546e84e4b92acbaf42ebf03c4ca5ac0cc2827))
- **(client-api)** Add /v1/me/* self-service + Stripe Checkout ([764a7b6](https://github.com/orkestra-cc/orkestra/commit/764a7b6c413a0ea710767c5012ea5a93c26acea6))
- **(openapi)** Self-mint OAuth JWTs for company + billing modules ([aea08ea](https://github.com/orkestra-cc/orkestra/commit/aea08eab845b6ceccf636c3ec5988001a892a0e2))
- D-10 devtoken --audience + integration smoke (ADR-0003 PR-D) ([76e10c5](https://github.com/orkestra-cc/orkestra/commit/76e10c534a82b855b5960d021d54b5e325e1c252))
- D-9 frontend cookie domain split (ADR-0003 PR-D) ([966aae8](https://github.com/orkestra-cc/orkestra/commit/966aae831401398e2b3f8d885adc9bfa32fb89e5))
- D-8 hard cutover — drop legacy auth paths + USER_TIER_SPLIT_ENABLED (ADR-0003 PR-D) ([ff4b089](https://github.com/orkestra-cc/orkestra/commit/ff4b08973d9ea897861bbacb07a42d0634e2b119))
- D-7 move client-tier routes to client surface (ADR-0003 PR-D) ([3b4f2fe](https://github.com/orkestra-cc/orkestra/commit/3b4f2fe2cd434759871e1792d16299699a078690))
- D-6 OAuth state-encoded tier dispatch (ADR-0003 PR-D) ([0716663](https://github.com/orkestra-cc/orkestra/commit/0716663e475a0c41d0d7555f3c74aab1c6c34536))
- D-5 client auth path split (ADR-0003 PR-D) ([ddce086](https://github.com/orkestra-cc/orkestra/commit/ddce086998570727eda07f63c71b11cadc85f0eb))
- D-4 operator auth path split (ADR-0003 PR-D) ([4f0cb44](https://github.com/orkestra-cc/orkestra/commit/4f0cb44558910f9b8f5df914fc0b0144886ac45e))
- D-3 JWT v2 cutover — aud mandatory (ADR-0003 PR-D) ([9036fea](https://github.com/orkestra-cc/orkestra/commit/9036feade47afea004cd544c18dc4e60a90a79f2))
- D-2 tier-aware auth services (ADR-0003 PR-D) ([837a700](https://github.com/orkestra-cc/orkestra/commit/837a70054a87b413cfc7d337e48650623764c001))
- D-1 tier-aware auth repositories (ADR-0003 PR-D) ([6f16163](https://github.com/orkestra-cc/orkestra/commit/6f1616308dd95a244fe2b452a08db8aa50f4be37))
- B-4 USER_TIER_SPLIT_ENABLED cutover gate (ADR-0003 PR-B) ([99266a9](https://github.com/orkestra-cc/orkestra/commit/99266a939f1c28ba6d798932da759e407b07c39a))
- B-3 migration script (ADR-0003 PR-B) ([0f14963](https://github.com/orkestra-cc/orkestra/commit/0f1496360c64a77e81def1c9e13f38ab4e330c07))
- B-2 tier-aware user providers (ADR-0003 PR-B) ([02b0c24](https://github.com/orkestra-cc/orkestra/commit/02b0c2465f16826459c73923c9a0f6b2900274c4))
- B-1 tier-split collection schemas (ADR-0003 PR-B) ([d85866e](https://github.com/orkestra-cc/orkestra/commit/d85866e6302ee894d4a5dd4dd1ca0f436c8e22f3))
- Per-host audience routing (ADR-0003 PR-C) ([eeb98e5](https://github.com/orkestra-cc/orkestra/commit/eeb98e57cfaab5f339ef2fb82e84ee53db29789e))
- **(billing,tenant)** Optional Customer.TenantUUID link (ADR-0001 PR-4) ([04df6da](https://github.com/orkestra-cc/orkestra/commit/04df6da601ae758617f8a650a007c80fec22257a))
- **(frontend)** Remove deprecated /subscriptions/clients page (ADR-0001) ([f6d95c0](https://github.com/orkestra-cc/orkestra/commit/f6d95c0c686b1ab79b0a61b73669076860b3f0f2))
- **(subscriptions,payments)** Complete ADR-0001 Phase 1 — drop legacy Client ([6db0e3f](https://github.com/orkestra-cc/orkestra/commit/6db0e3f35f7d2e1b5e056aa9ed3a2e6d9b4d5899))
- **(docker)** Hot-reload staging (AIR + Vite HMR) ([b75240c](https://github.com/orkestra-cc/orkestra/commit/b75240cf79200bc00aed215e1345035041edf734))
- **(auth,notification)** Suspicious-login email + security events (C5) ([68529ab](https://github.com/orkestra-cc/orkestra/commit/68529aba2cde4b55bf9a76968c6bea1863a67a5e))
- **(auth)** Impossible-travel factor + GeoIP plumbing (C4 of Section C) ([3962853](https://github.com/orkestra-cc/orkestra/commit/39628534456f54ebf7be22dc9ee624633162df28))
- **(auth)** Device trust — "remember this device 30 days" (C3 of Section C) ([45e15e3](https://github.com/orkestra-cc/orkestra/commit/45e15e37d280b3e0f876cc8c0e85374a019d552a))
- **(auth,authz)** Wire risk score to step-up + Cedar ABAC (C2 of Section C) ([1f3725c](https://github.com/orkestra-cc/orkestra/commit/1f3725c83f30dd861e6b24b5ed5832fed42d5b6c))
- **(auth)** Honest login-risk scorer (C1 of Section C) ([5e886e8](https://github.com/orkestra-cc/orkestra/commit/5e886e82ecd70f6bb4f81e4f4f9fe744fb64e12d))
- **(authz)** First ABAC policies — MFA + public-IP forbids (C of B#4) ([138dfa5](https://github.com/orkestra-cc/orkestra/commit/138dfa57f764f2f200fb8a40368a4ae55e18a5ee))
- **(authz)** Cedar ABAC attrs — time-of-day + IP bucket (B of B#4) ([fc0109c](https://github.com/orkestra-cc/orkestra/commit/fc0109ccf93665b5f25878c270830928d0c5022c))
- **(authz)** Cedar ABAC attrs — tenant status + MFA signals (A of B#4) ([8fee818](https://github.com/orkestra-cc/orkestra/commit/8fee818d097bae9284bae2f779c50cc0230ea3c2))
- **(authz)** Cascade + system/tenant separation in CreateBinding (commit C of org-role split) ([c5151d2](https://github.com/orkestra-cc/orkestra/commit/c5151d263d2edd027ced0fe1684aaa9e6f75d619))
- **(authz)** Bind org_owner on tenant create (commit B of org-role split) ([84981ac](https://github.com/orkestra-cc/orkestra/commit/84981ac92ff605574546b0248b39013f07b7c68f))
- **(authz)** Seed 5 org roles + Cedar policies (commit A of org-role split) ([b6da3d9](https://github.com/orkestra-cc/orkestra/commit/b6da3d91fb793c2d14265a783253405425e8198d))
- **(authz)** Add Cedar enforce mode for opt-in actions ([f8d2f48](https://github.com/orkestra-cc/orkestra/commit/f8d2f48498288b1d91a34d6b823a91117903b93d))
- **(auth)** WebAuthn passkeys close out Section A ([860c5f1](https://github.com/orkestra-cc/orkestra/commit/860c5f197a5af67d2848b4ece9d06f880f009375))
- **(auth)** Land Section A — revocation, step-up, MFA UX ([8fb2fdf](https://github.com/orkestra-cc/orkestra/commit/8fb2fdf0de5d07a918a35d851cb34589c5b2f88d))
- **(nav)** Render realm → section sidebar hierarchy ([f6db708](https://github.com/orkestra-cc/orkestra/commit/f6db70884b164bbbf7d935f542dcbb24b9e643c4))
- **(navigation)** Add Realm/Section/Tier filtering to nav ([e33acd8](https://github.com/orkestra-cc/orkestra/commit/e33acd80f9bd92db0d0b994d19a41cf12490baf7))
- **(tenancy)** Two-tier split — internal ops vs client mgmt ([c9be9e2](https://github.com/orkestra-cc/orkestra/commit/c9be9e223d9bb36242bfd503e32e710914beb426))
- **(tenancy)** Admin tenant impersonation with audit ([c4fea0a](https://github.com/orkestra-cc/orkestra/commit/c4fea0a08fd9bb39c830eeb7022d6d7948645a95))
- **(ci)** Phase 5.4 — tenantscope v2 (core+shared coverage, allow-until) ([736faaf](https://github.com/orkestra-cc/orkestra/commit/736faaf5dcf6b8c4ffc5b4584448ca6b464cf551))
- **(observability)** Phase 5.3 — Prometheus /metrics + ADR-0002 ([ee013fb](https://github.com/orkestra-cc/orkestra/commit/ee013fbc516f035beeb58eb0718a8da953a51292))
- **(observability)** Phase 5.2 — OTEL stack + baggage coverage test ([7decb97](https://github.com/orkestra-cc/orkestra/commit/7decb97d51d70bc046f94b29bdaf605096ea72de))
- **(ci)** Phase 5.1 — policy coverage report + CI gate ([227e161](https://github.com/orkestra-cc/orkestra/commit/227e16137e03bbc865ce957b800d3c8cf4b2fd74))
- **(identity)** Phase 4.5.5 — per-tenant IdP + SCIM admin ([161d805](https://github.com/orkestra-cc/orkestra/commit/161d8055e8026865fbc12ff4697086a5939970f0))
- **(tenant)** Phase 4.5.4 — admin purge (crypto-shred) action ([2a39e22](https://github.com/orkestra-cc/orkestra/commit/2a39e22ae06fcdcd4c631f021c4613ff576d4445))
- **(compliance)** Phase 4.5.3 — user privacy (DSR export + erase) ([e9d703f](https://github.com/orkestra-cc/orkestra/commit/e9d703f5fce5e66e88eb7a07bbaccac81f3dc8f2))
- **(compliance)** Phase 4.5.2 — SOC2 evidence page ([3a05993](https://github.com/orkestra-cc/orkestra/commit/3a05993741672d9e35a8dc1ddb0c6049d97aa10e))
- **(compliance)** Phase 4.5.1 — admin audit-events page ([a04df23](https://github.com/orkestra-cc/orkestra/commit/a04df23e128bc444e40fe157592f6673c10f9681))
- **(compliance)** Phase 4.4 — SOC2 evidence + OTEL tenant baggage (exit criterion B) ([d406942](https://github.com/orkestra-cc/orkestra/commit/d4069424a1105d68150bb975dadf9f5c35f9a908))
- **(compliance)** Phase 4.3 — per-tenant KMS envelope + crypto-shred on purge ([c3d210c](https://github.com/orkestra-cc/orkestra/commit/c3d210cf0422be9db9e892795ac747b0ea721e6a))
- **(compliance)** Phase 4.2 — GDPR DSR pipeline (exit criterion A) ([588255e](https://github.com/orkestra-cc/orkestra/commit/588255ee0480eb577eeaf97b18c842b58c931a3f))
- **(compliance)** Phase 4.1b — extend audit emit to identity + subscriptions ([f1381c0](https://github.com/orkestra-cc/orkestra/commit/f1381c0adcd954b36a51422798821e9c55bf53da))
- **(compliance)** Phase 4.1 — append-only audit log foundation ([bc0b913](https://github.com/orkestra-cc/orkestra/commit/bc0b913468e68d897b8e65f0adc2b7010129bd56))
- **(identity)** Phase 3.4 — SCIM 2.0 endpoint stubs ([e2c425b](https://github.com/orkestra-cc/orkestra/commit/e2c425bddc7b571d203b5a956019f78c32b556ac))
- **(identity)** Phase 3.3 — BYO OIDC identity module ([b871e23](https://github.com/orkestra-cc/orkestra/commit/b871e23d121e7ad92d257ff40476e5b5026cba99))
- **(tenancy)** Phase 3.2 — activate-on-verify + self-subscribe ([9bc1074](https://github.com/orkestra-cc/orkestra/commit/9bc10746f53ddf4decb99de784d7b177ad1f55fb))
- **(onboarding)** Phase 3.1 — anonymous signup scaffold ([e32d5bf](https://github.com/orkestra-cc/orkestra/commit/e32d5bf21d9dc92ea8dadff8650fccb303563150))
- **(tenancy)** Phase 2 exit — capability-gated routes ([38ea6eb](https://github.com/orkestra-cc/orkestra/commit/38ea6eb95f1c629a4ad0e4b1d0c79489ef72810a))
- **(tenancy)** Phase 2.3 — RequireCapability middleware returning 402 ([01fe610](https://github.com/orkestra-cc/orkestra/commit/01fe6102ebecb9456d73e1dd8f595ad510b6f563))
- **(tenancy)** Phase 2.2 — wire subscription state to capability projection ([51ed50f](https://github.com/orkestra-cc/orkestra/commit/51ed50f8d73b6d1bcd04b28d0f3c26bf70b6a987))
- **(tenancy)** Phase 2.1 — capability catalog + entitlement projection ([e972e8a](https://github.com/orkestra-cc/orkestra/commit/e972e8ac341ee4a62e48bdbc06e9f8459c4c442e))
- **(authz)** Phase 1 — Cedar policy engine in shadow mode ([5e2e3ce](https://github.com/orkestra-cc/orkestra/commit/5e2e3cef0d8fe6597dcbc08a0d1326da7623c156))
- **(tenancy)** Phase 0 — unified Tenant model foundations (ADR-0001) ([2218221](https://github.com/orkestra-cc/orkestra/commit/2218221659de123bc6bea71b35c6a3a0e544d629))
- **(auth)** Refresh-token family + replay detection — Block C ([4a03068](https://github.com/orkestra-cc/orkestra/commit/4a03068ec4beb7dc2419daca714930a0d2b21481))
- **(auth)** Enforce MFA at login + gate sensitive routes — Block B ([afe2a19](https://github.com/orkestra-cc/orkestra/commit/afe2a190dacc285b9fc4aeda3cc8a9264c3f4d7c))
- **(auth)** Ship MFA (TOTP) foundations — Phase 1 Block A ([128f466](https://github.com/orkestra-cc/orkestra/commit/128f466f93bc21b194462020df18239885ce830c))
- **(rbac)** Foundation for org-scoped RBAC (phases 0-1) ([abb1f65](https://github.com/orkestra-cc/orkestra/commit/abb1f656fbb086f343a748915186c8c9f071345a))
- **(graph)** Backend-managed Memgraph container ([d3584e9](https://github.com/orkestra-cc/orkestra/commit/d3584e9f9c693aa24b1433f6941474698f3ed966))
- **(modules)** Add subscriptions + payments addons ([293045a](https://github.com/orkestra-cc/orkestra/commit/293045a02f02f0ba2880a28612493745d1be0c99))
- **(modules)** Auto start/stop infra containers ([ccf4178](https://github.com/orkestra-cc/orkestra/commit/ccf4178ad54aafdc18d1ad7501aa894fb01c0c53))
- **(modules)** Hot-reload enable/disable without restart ([cfef915](https://github.com/orkestra-cc/orkestra/commit/cfef91568646dbe2740a88f98737252b0c1c4569))
- **(aimodels)** Provider-tabbed admin config page ([095a781](https://github.com/orkestra-cc/orkestra/commit/095a781d65d834837bd9e124ea4e2001baefbf3c))
- **(aimodels)** Hot-reload config and add enable/disable toggle ([10d95ed](https://github.com/orkestra-cc/orkestra/commit/10d95ed40a9e38b064699a68d755605775fd5f92))
- **(frontend)** Sync tab state with URL search params ([bc76699](https://github.com/orkestra-cc/orkestra/commit/bc766994d78f87ff11271fe31f91e1cef58f4691))
- **(modules)** Show active environment in admin modules table ([0db0452](https://github.com/orkestra-cc/orkestra/commit/0db0452cdd7867804fa1c654b46b23fd465c2aec))
- **(modules)** Full-page settings with environment profiles ([38bc4c0](https://github.com/orkestra-cc/orkestra/commit/38bc4c04167cf2dd68b0ed34f7c6a3f9a88325aa))
- **(ci)** Harden CI/CD quality gates and add security scanning ([6f1aa24](https://github.com/orkestra-cc/orkestra/commit/6f1aa24e17e8797f5f24f824b0dce59b63ce5f40))
- **(modules)** DB-driven module loading + pending_restart status ([0432c36](https://github.com/orkestra-cc/orkestra/commit/0432c36c501315df84cc43255f0a5248ddffa908))
- **(auth)** Runtime OAuth config + admin-panel tabs ([cfc631c](https://github.com/orkestra-cc/orkestra/commit/cfc631c0c75738101e285fe3f367f08c85ef7ae7))
- **(tenant)** Platform-admin tenant management UI + API ([d0be8ad](https://github.com/orkestra-cc/orkestra/commit/d0be8adaf9230ed1444ad88d6d44347b2bdb9ba8))
- **(modules)** Show full addon catalog in admin UI ([15bb295](https://github.com/orkestra-cc/orkestra/commit/15bb29539c4f4652dc6c8ca531426510854371b0))
- **(core)** 6-role model + self-heal seeds ([d5333f0](https://github.com/orkestra-cc/orkestra/commit/d5333f05ba7c86588f6e0231e337780814675ff6))
- **(authz)** Editable, cascade-safe role management UI ([e4ecb63](https://github.com/orkestra-cc/orkestra/commit/e4ecb63635dfde6f4ccb49fe73f91178c724f543))
- **(setup)** Add organization step to first-install wizard ([22de7ec](https://github.com/orkestra-cc/orkestra/commit/22de7ec8fb8e7fd9466f569d349892e32ba558da))
- **(setup)** First-install onboarding wizard ([0e7a964](https://github.com/orkestra-cc/orkestra/commit/0e7a96483408978bc92ad201db13a1561c377703))
- **(authz)** Role Management admin UI + fix loader spin ([5c78e6f](https://github.com/orkestra-cc/orkestra/commit/5c78e6f166538d6226dcf49c371011f8fd9f530e))
- **(rbac)** Multi-tenant RBAC with permissions, orgs, and entitlements ([0f5dca6](https://github.com/orkestra-cc/orkestra/commit/0f5dca6d0bfdb5c5f1a733aeaa888617e35f65b5))
- **(frontend)** Email/password auth UI with OAuth as secondary ([6fb4d43](https://github.com/orkestra-cc/orkestra/commit/6fb4d43373f42f01afbe1afffa1777d888246065))
- **(auth)** Email/password authentication with argon2id + verification ([dd2d169](https://github.com/orkestra-cc/orkestra/commit/dd2d16944354347515f8c9c00eaa9a8103794d87))
- **(notification)** Add core notification module with SMTP email channel ([1c89ce9](https://github.com/orkestra-cc/orkestra/commit/1c89ce925ac0c0c5c9c0e49ccf34dca4b7d80b6d))
- **(scripts)** Unify stack management into orkestra.sh ([9218187](https://github.com/orkestra-cc/orkestra/commit/921818772e1fe82ecec92bddb2ee4251acbb0ef5))
- Minimal bootstrap profile + db startup reliability ([a9a13d4](https://github.com/orkestra-cc/orkestra/commit/a9a13d479bacf2451da1d80cacac12bf3c756663))
- Add Module Management dashboard with admin API enhancements ([b4ddf57](https://github.com/orkestra-cc/orkestra/commit/b4ddf5723e0b3a68df44d1bff0284a2636d8a05d))
- **(backend)** DB-backed config override for module Init() ([1d6a6c9](https://github.com/orkestra-cc/orkestra/commit/1d6a6c9d4d830669130a37a898775c64ed5f3812))
- **(backend)** Dynamic navigation from module declarations (Phase 4) ([9ab82d6](https://github.com/orkestra-cc/orkestra/commit/9ab82d63040a632029da0b27722b28e415766101))
- **(backend)** Admin module management API (Phase 3) ([56cd776](https://github.com/orkestra-cc/orkestra/commit/56cd7760173f670370397d35bf9ed5d81f9c8dbd))
- **(backend)** Self-contained module system — Phase 2 infrastructure ([77b6c9c](https://github.com/orkestra-cc/orkestra/commit/77b6c9c1f5e2ca16c0950321d96aa153d1074e93))
- **(backend)** Self-contained module system — Phase 1 ([742967e](https://github.com/orkestra-cc/orkestra/commit/742967e260cb471c7b0d82cc18ae44457daf2d05))
- **(backend)** Add Module interface and registry (Phase 1) ([db1d73a](https://github.com/orkestra-cc/orkestra/commit/db1d73a518bdaf91a3e051ac6c57427fb775cacb))
- **(frontend)** Add resize handle and graph panel styles ([936a938](https://github.com/orkestra-cc/orkestra/commit/936a9384155c3b4bc2989ab7adcdc9277be9d7d9))
- **(sales)** Add batch API support for prospect pipeline ([bd61ec4](https://github.com/orkestra-cc/orkestra/commit/bd61ec41095218d90b03e22d2dadd7a77dfcd401))
- **(sales)** Async skills, locale support, and settings consolidation ([01e66c9](https://github.com/orkestra-cc/orkestra/commit/01e66c9bf5d1e6c27293b6d1086f6a7b974c332d))
- **(sales)** Add prompt management, delete, and agent re-run ([07c77c9](https://github.com/orkestra-cc/orkestra/commit/07c77c994fa0a8c3fe676114e8f4ea35540e9aff))
- **(sales)** Add AI Sales Intelligence module ([d31de62](https://github.com/orkestra-cc/orkestra/commit/d31de6220167bd8cb748b925b1f278634f32033a))
- **(agents)** Add per-user personal agent with Hindsight memory ([3ad1b08](https://github.com/orkestra-cc/orkestra/commit/3ad1b086877ca433e84dd650fc4ee7b5bff743ec))
- **(rag)** Add cross-document similarity and relations endpoint ([54a677b](https://github.com/orkestra-cc/orkestra/commit/54a677be53676541d0a6a2aa42f5abd3e7a3c86d))
- **(agents)** Add per-project settings and token usage tracking ([27ad0c8](https://github.com/orkestra-cc/orkestra/commit/27ad0c89ef99b4830f1e8b6415f2cf804da66ab2))
- **(agents)** Add Hindsight AI agent module with project-scoped RAG ([bc79c2b](https://github.com/orkestra-cc/orkestra/commit/bc79c2b6a9b4b61b6cb01500c1ba2f2b841a9c06))
- **(rag)** Improve chunking, add contextual retrieval, and LLM model selection ([f0d260f](https://github.com/orkestra-cc/orkestra/commit/f0d260f2b3a23d7ab887ece7b0297d8ff0b82847))
- **(graph)** Add document scope filter and sync editor on sidebar clicks ([f756e1b](https://github.com/orkestra-cc/orkestra/commit/f756e1b7405a2e110d0a02bc9573dd65213d9bd9))
- **(graph)** Resizable panels, collapsible sidebar, and JSON node viewer ([810512a](https://github.com/orkestra-cc/orkestra/commit/810512aa8b5fab21d4e15ecc4e9522cd784f90c0))
- **(rag)** Add goldmark-based Markdown document parser ([55bbe20](https://github.com/orkestra-cc/orkestra/commit/55bbe201231f9c5df12e3e3cb977049bb8a82424))
- **(rag)** Add relationship types CRUD and Markdown parsing ([63598b0](https://github.com/orkestra-cc/orkestra/commit/63598b0b81fdc96b3c4429a3b782401af8c4ab7a))
- **(rag)** Add structural document parsing and rich graph relationships ([d3b8752](https://github.com/orkestra-cc/orkestra/commit/d3b8752e95073072f931d87f8f7cdfc2421b3fcd))
- **(graph,rag,aimodels)** Add document CRUD, content viewer, and model filtering ([5ef4b58](https://github.com/orkestra-cc/orkestra/commit/5ef4b58af97e0b907e4cf41a871225730819bcbd))
- **(aimodels)** Extract AI model management into standalone module ([4f72fdd](https://github.com/orkestra-cc/orkestra/commit/4f72fdd0a0aabab8658f9285faf88293dc6901d3))
- **(rag)** Add SSE streaming for RAG queries ([808392b](https://github.com/orkestra-cc/orkestra/commit/808392bf0ad379f9944d34c30b652f1cc42e24b1))
- **(graph,rag)** Migrate to Memgraph and add Graph RAG system ([fabaf35](https://github.com/orkestra-cc/orkestra/commit/fabaf35051d9b9b493e16b9e792e317432e8035b))
- **(graph)** Add Neo4j graph database module with visual explorer ([d329eba](https://github.com/orkestra-cc/orkestra/commit/d329ebae26c42f9a935a920d24d09b2be4521509))
- **(company)** Add company search page and IT-search API endpoint ([7578ce7](https://github.com/orkestra-cc/orkestra/commit/7578ce7f1ea8860f3eb65601b69ada30174c7220))
- **(company)** Extend advanced enrichment data and add status column ([1f13b61](https://github.com/orkestra-cc/orkestra/commit/1f13b61dec0574eb575296968c57c6d8bf4215e1))
- **(company)** Add enrichment API and structured enrichment UI ([0eb8165](https://github.com/orkestra-cc/orkestra/commit/0eb8165efc7b5fd5f2bbffd6b19c785fe6ec21f3))
- **(company)** Add company lookup module via OpenAPI Company API ([54892aa](https://github.com/orkestra-cc/orkestra/commit/54892aa81e7cdb488497b6489ed7b25b52df9d76))
- **(billing)** Add SDI webhooks, fix invoice sync, and improve observability ([a4c4d02](https://github.com/orkestra-cc/orkestra/commit/a4c4d02c98d3c924daf240aad006c4da01fbd903))
- **(billing)** Add company selector for payment data auto-fill ([0a767ba](https://github.com/orkestra-cc/orkestra/commit/0a767bade1e9227a6992c90a898e01e4f5096b5c))
- **(billing)** Track legal storage settings and preserved document ID ([e957433](https://github.com/orkestra-cc/orkestra/commit/e9574338b8e3a6ad4089a103801929c9c109b9a3))
- **(billing)** Add invoice duplication and fix stamp duty calculation ([bfb841c](https://github.com/orkestra-cc/orkestra/commit/bfb841c8e8ce81eab1656bdd9ce0cf29ce0e5194))
- **(billing)** Delete SDI business registry on company deletion ([7ebc6fe](https://github.com/orkestra-cc/orkestra/commit/7ebc6fe9cc5750bac7ca4a8baf4240949c40e7c3))
- **(billing)** Add manual SDI sync button to issued invoices ([ab03dc7](https://github.com/orkestra-cc/orkestra/commit/ab03dc73db15d2ffff56d02812a22b127bb00230))
- **(billing)** Add RiferimentoNormativo for RF19 and fix TD04 payment block ([ff0e271](https://github.com/orkestra-cc/orkestra/commit/ff0e271cd95b0bba19fbe0c3b2bc97868291cb51))
- **(billing)** Add isProfessional flag and forfettario (RF19) support ([9f20102](https://github.com/orkestra-cc/orkestra/commit/9f2010273592d7fdec874ceb752b23396533c417))
- **(billing)** Add credit note (TD04) creation from issued invoice ([bda2225](https://github.com/orkestra-cc/orkestra/commit/bda22255ca59cc0c86af6a51b1458c6d27a3fd4c))
- **(billing)** Reduce OpenAPI SDI API calls with caching and 12h polling ([3d27f5f](https://github.com/orkestra-cc/orkestra/commit/3d27f5f85af3810339fd4ed54ac4b710e07b4f82))
- **(billing)** Change invoice trend chart to weekly bar chart ([5ac9bb6](https://github.com/orkestra-cc/orkestra/commit/5ac9bb6f95df1c9fd869dc929dddec6771e9f9e3))
- **(billing)** Add virtual stamp duty note to invoice PDF ([0a51875](https://github.com/orkestra-cc/orkestra/commit/0a51875866801c061b88c75cc7b08cec5b969a33))
- **(billing)** Add native FatturaPA XML import for received invoices ([1e33b18](https://github.com/orkestra-cc/orkestra/commit/1e33b187c1a7eb24fadfc9164b9f324c8b2e98d6))
- **(billing)** Sync both issued and received invoices from OpenAPI SDI ([7036938](https://github.com/orkestra-cc/orkestra/commit/703693835614f33f9ca57db80a6bdd75ca0f3f8c))
- **(billing)** Add PDF generation for draft invoices ([24d7635](https://github.com/orkestra-cc/orkestra/commit/24d763554cd73c51c3538b4e3ced2c52012878bc))
- **(documents)** Add PDF generation service with Gotenberg ([3b21a0c](https://github.com/orkestra-cc/orkestra/commit/3b21a0c2832e0c38f68651c9671a03d2b1c8d0c2))
- **(dev)** Add CLI-based JWT token generator for testing ([13123f8](https://github.com/orkestra-cc/orkestra/commit/13123f8ea52e86e30a7d61bfe852b9cff9270692))
- **(billing)** Integrate Business Registry into Company modal and sync received invoices ([cf908fe](https://github.com/orkestra-cc/orkestra/commit/cf908fecc0bd5810f25db5f3e5dd17ecc8c08259))
- **(billing)** Add Business Registry configuration API and UI ([3317e97](https://github.com/orkestra-cc/orkestra/commit/3317e97bec4f1296544825c688433671af1b5c54))
- **(billing)** Add AltriDatiGestionali support and FatturaPA filename compliance ([2121b47](https://github.com/orkestra-cc/orkestra/commit/2121b478c423acb8d9e79be400b8dcf5e826b78e))
- **(billing)** Add multi-company management for invoice emission ([cebea61](https://github.com/orkestra-cc/orkestra/commit/cebea61ab022efb2407ebc3230b36b4b99e7247c))
- **(billing)** Add FatturaPA XSD compliance and frontend support ([6b9ea44](https://github.com/orkestra-cc/orkestra/commit/6b9ea449a36ca48930e0824f6118dcde1311dd2f))
- **(auth)** Assign developer role to first user ([beee325](https://github.com/orkestra-cc/orkestra/commit/beee3250605b51bd0fdfbd41e471f244528fdd7a))
- **(billing)** Add issued invoice detail page ([54a61e0](https://github.com/orkestra-cc/orkestra/commit/54a61e0d7636154fd742a7b480ea469fa9e7a6be))
- **(billing)** Wire billing module into main server ([aa0978d](https://github.com/orkestra-cc/orkestra/commit/aa0978d4f53b40e1c5fd90fdcf22e5a835747abc))
- **(billing)** Add Italian electronic invoicing module ([091acd3](https://github.com/orkestra-cc/orkestra/commit/091acd3081f5212e5c0e5529e8b72909418f8ff0))
- **(navigation)** Implement dynamic backend-driven navigation menu ([76e012d](https://github.com/orkestra-cc/orkestra/commit/76e012d91c4605daa4a2a747dbea12443d48b5d7))
- **(auth)** Allow server startup without JWT keys in development ([f4955e8](https://github.com/orkestra-cc/orkestra/commit/f4955e890253bb22a3878e551f617a147810003e))

### Bug fixes

- **(marketing)** Bind-mount spool dir for write access under userns-remap ([6552bb8](https://github.com/orkestra-cc/orkestra/commit/6552bb8d067317421b0f66b52fbfc5aac9695bbc))
- **(marketing)** Writable spool dir in dev/staging compose ([65da193](https://github.com/orkestra-cc/orkestra/commit/65da1931eb38f6a5f2e1bf21cf29444a41abdd88))
- **(frontend-admin)** Gitignore public/config.js, generate per env ([1656a18](https://github.com/orkestra-cc/orkestra/commit/1656a185ba629286a77da67d3009fdfb9aef2c0d))
- **(docker)** Staging frontend reads its own runtime config (not dev defaults) ([aabbf90](https://github.com/orkestra-cc/orkestra/commit/aabbf90fe5977e76cbb184e048b6afd3d868cfe9))
- **(logging)** Annotate log_levels Mongo calls as scope-exempt ([ed3e4ce](https://github.com/orkestra-cc/orkestra/commit/ed3e4cebccb70f49412068ef93851cc7a08c1701))
- **(observability)** Unblock Prometheus scrape of backend /metrics ([d07dadf](https://github.com/orkestra-cc/orkestra/commit/d07dadff7d179c0b2f4c4bad571471da5a2d5bc0))
- **(observability)** Ship OTLP logs end-to-end on staging (ADR-0005 Phase E) ([d8ef81c](https://github.com/orkestra-cc/orkestra/commit/d8ef81cc6842a9522a9921e5c9fb5756a6d4f73f))
- **(orkestra-sh)** Show profile menu when ENV set; resolve logs by label ([1321c46](https://github.com/orkestra-cc/orkestra/commit/1321c4627de1810570bf365574a57d050ca038f0))
- **(backend)** Satisfy gosec on OPENAPI_DUMP path handling ([db1dc17](https://github.com/orkestra-cc/orkestra/commit/db1dc17ec87422ba580b19285f3b07e7836c6d42))
- **(auth)** Gate OAuth signup on registrationEnabled + default new signups to guest ([df24cb2](https://github.com/orkestra-cc/orkestra/commit/df24cb2de7dbafc8e07eba8f24bf37b7c015f5de))
- **(docker)** Pin literal names on infra mongo/redis volumes ([902b499](https://github.com/orkestra-cc/orkestra/commit/902b499b823300ac578854557ae7db621e007e55))
- **(docker)** Chain SKU frontend env vars through existing VITE_* / BACKEND_URL ([7ec91da](https://github.com/orkestra-cc/orkestra/commit/7ec91da38677f10fe9774af10fe58bfc1249394a))
- **(docker)** Forward ALLOW_LOCALHOST_REDIRECTS + OAuth creds in SKU composes ([ad76f60](https://github.com/orkestra-cc/orkestra/commit/ad76f60c1273596ed17410b731afd2000c7863ad))
- **(ci)** Go-version-file is repo-root-relative, not working-directory-relative ([6009519](https://github.com/orkestra-cc/orkestra/commit/6009519324f6e9456aeae1ba01da7d894e46e095))
- **(frontend-admin)** Retarget legacy nav exports at /reference/* paths ([0ed1e7c](https://github.com/orkestra-cc/orkestra/commit/0ed1e7c764ef55af41d8da89580b0600920b9b6c))
- **(ci)** Pin Go version from go.work + bump golangci-lint-action ([bd72c05](https://github.com/orkestra-cc/orkestra/commit/bd72c0500760a75c1fd87be99190d790b95d5806))
- **(mobile)** Scaffold Android platform so Mobile CI builds APK ([de4fab5](https://github.com/orkestra-cc/orkestra/commit/de4fab5455ddd8db0a9bf72ae2be3a938d33ef43))
- **(docker)** Copy source before `go mod download` so addon replaces resolve ([0fedfb7](https://github.com/orkestra-cc/orkestra/commit/0fedfb7acd24795fee8f95f7e0631ccb94bb8a6e))
- **(policycoverage)** Baseline tenant.update — consumed by extracted identity addon ([5d54554](https://github.com/orkestra-cc/orkestra/commit/5d545546e5a5cfa93996a1a7443db5550f0e0e5e))
- **(ci)** Run go tool cover from backend module dir ([ecb14e1](https://github.com/orkestra-cc/orkestra/commit/ecb14e161c7fc45d8816c9b80905ac017c1029ee))
- **(ci)** Match golangci-lint version + apply pending prettier formatting ([df7ab6d](https://github.com/orkestra-cc/orkestra/commit/df7ab6d1df068bcae177cb5d81a5dc6aa07e61dd))
- **(frontend-admin)** Update leaflet.tilelayer.colorfilter to v2 API ([6a22985](https://github.com/orkestra-cc/orkestra/commit/6a229859e426b1d1f2f58285c43a5e1bc9efbe9d))
- **(auth)** Unstick step-up for users without MFA enrolled ([85ef9b3](https://github.com/orkestra-cc/orkestra/commit/85ef9b37ebc1c323e76a545f4cb887a3219c076c))
- **(mobile)** Use package: imports so flutter analyze passes ([003cc75](https://github.com/orkestra-cc/orkestra/commit/003cc753f9311f511fb117c812b026b2b5f287af))
- **(admin-modules)** Respect schema default on bool config switch ([0046c16](https://github.com/orkestra-cc/orkestra/commit/0046c1695d01a7b1488b9ee47dea655518d9ca7a))
- **(operator)** Correct guest visibility for nav and self profile ([f2554b8](https://github.com/orkestra-cc/orkestra/commit/f2554b89b46d5d00939adfd339db9b92bc05d8db))
- **(auth)** Route OAuth web callback to /mfa/verify on MFA partial ([0425f28](https://github.com/orkestra-cc/orkestra/commit/0425f281a8eee50f37e40a1afc1dc9db928e6363))
- **(billing)** Drop dead /notifications poll causing 404 spam ([85c3e0c](https://github.com/orkestra-cc/orkestra/commit/85c3e0caa731eff47b7a47689a1ea0f7020cbc21))
- **(orkestra-sh)** Mark executable bit in git index ([043bc79](https://github.com/orkestra-cc/orkestra/commit/043bc799b1db0bf2a91b9165edc90080964161af))
- **(rag)** Release LLM stream context via StreamResult.Cancel ([d3127f9](https://github.com/orkestra-cc/orkestra/commit/d3127f91d32e07451b85e1a2e1e42c8214eb4b0a))
- **(frontend-admin)** Use card-header-tabs on detail-page tabs ([c4853a2](https://github.com/orkestra-cc/orkestra/commit/c4853a2091846e2111bf3ebe4600cf88144c9e02))
- **(notification)** RFC 2045 encode email body parts ([5c8bc97](https://github.com/orkestra-cc/orkestra/commit/5c8bc97ce42c1827783f5ac8746be58db7492b38))
- **(auth)** Unify MFA 401 code as step_up_required ([1ba017e](https://github.com/orkestra-cc/orkestra/commit/1ba017e3736ba57dbe9358b94b5d1835d69aaf30))
- **(frontend-client)** Skip refresh bootstrap for anonymous visitors ([b13a0e3](https://github.com/orkestra-cc/orkestra/commit/b13a0e3d4dae1fe032024d0a023aaebddc6232c3))
- **(frontend)** Align MFA status with backend, fix verify→dashboard ([60790e5](https://github.com/orkestra-cc/orkestra/commit/60790e51644a65bf7f231143925a3eeac779bb31))
- D-9 frontend auth paths to operator tier (ADR-0003 PR-D) ([7106aaa](https://github.com/orkestra-cc/orkestra/commit/7106aaa4b37b2f1099e9b668f509294073a66df2))
- Serve /health for HAProxy LAN probes ([2a62f79](https://github.com/orkestra-cc/orkestra/commit/2a62f790f95c09bd74de52be7853973a92fc088f))
- **(tenant)** Memoize selectImpersonation selector ([028b592](https://github.com/orkestra-cc/orkestra/commit/028b59208e87a5544e3d6e8fa89401c2aa0f82cf))
- **(frontend)** Clean up console errors and warnings ([68db696](https://github.com/orkestra-cc/orkestra/commit/68db696c4668115ecaf0ac7871e4b036a116a764))
- **(auth)** Tolerate multiple refresh cookies + targeted tenant-switch invalidation ([cf8fbec](https://github.com/orkestra-cc/orkestra/commit/cf8fbec1de0370653a401176e683e568cb83e1f6))
- **(auth)** Gate tenant-scoped queries on access token presence ([0781964](https://github.com/orkestra-cc/orkestra/commit/078196425a961624069dc08addc8777e6f99765f))
- **(auth)** Eliminate post-login /session refetch race ([39b19d7](https://github.com/orkestra-cc/orkestra/commit/39b19d73fb8eb5f4b92a393def615dbb82c08831))
- **(auth)** Honor JWT TTL env vars + silent refresh on 401 ([0639027](https://github.com/orkestra-cc/orkestra/commit/0639027dc2ee7ddc1115bbbdb02d62ad8b3e0c53))
- **(identity ui)** Treat 404 as not-configured empty state ([9586054](https://github.com/orkestra-cc/orkestra/commit/95860541d83191c85ca839e08dd18dfadccb00b7))
- **(cors)** Allow X-Tenant-ID on preflight ([349616f](https://github.com/orkestra-cc/orkestra/commit/349616fc63da28dcc83f802c0240f9cafa878249))
- **(identity)** Gate IdP + SCIM admin on tenant.update ([191a1e6](https://github.com/orkestra-cc/orkestra/commit/191a1e68139b8ea12dab35ca92ffad95e5a91c66))
- **(payments,subscriptions)** Close critical audit gaps ([234681f](https://github.com/orkestra-cc/orkestra/commit/234681f5fe0ff629f5e9accb1655a19972462e93))
- **(frontend)** Register missing sidebar icons ([dfc0193](https://github.com/orkestra-cc/orkestra/commit/dfc0193acbdd7fe28078d3700702b94b6aafa691))
- **(admin-modules)** Refresh health dot on toggle ([ef8bf56](https://github.com/orkestra-cc/orkestra/commit/ef8bf56761aa0983931e624108ee4958b2acb95f))
- **(sales)** Propagate quick prospect errors ([7a84e62](https://github.com/orkestra-cc/orkestra/commit/7a84e62013cb76c1264125049fd5790952f566cd))
- **(modules)** Refresh all code-derived metadata on reseed ([7057f03](https://github.com/orkestra-cc/orkestra/commit/7057f030939851560d2568f9cf190f057cc02bff))
- **(compose)** Unify hindsight volume with backend ([210fc88](https://github.com/orkestra-cc/orkestra/commit/210fc88dd59917d23e43f8678cff04dc760e9f90))
- **(billing)** Hot-reload config and fix encryption key ([9be8492](https://github.com/orkestra-cc/orkestra/commit/9be8492b39a590223cb66e3cdb6d9933455cdef6))
- **(frontend)** Register missing FontAwesome icons for billing nav ([057e14c](https://github.com/orkestra-cc/orkestra/commit/057e14ca0f2737e8691ce7be263fad0ad71d80ef))
- **(authz)** Resolve dev-token permission failure ([fdf9b63](https://github.com/orkestra-cc/orkestra/commit/fdf9b6392a5a5e928d367b80cfb027c949ca748d))
- **(modules)** Prevent browser autofill on admin search input ([d5fb365](https://github.com/orkestra-cc/orkestra/commit/d5fb365355f2c1ed5f42346f7ccd8ff2d6f7d094))
- **(authz)** Sort system roles by privilege rank ([93d36e5](https://github.com/orkestra-cc/orkestra/commit/93d36e5fd0481059fe03fd007f739f4f4388b7bf))
- **(backend)** Use registered FontAwesome icon for Module Management nav ([4c93666](https://github.com/orkestra-cc/orkestra/commit/4c93666d6e643464851baf1cbe8f802494936d4e))
- **(backend)** Missing time import and stale cfg refs in module Init() ([5040c94](https://github.com/orkestra-cc/orkestra/commit/5040c94dd0fc4e137352aa03f3d50a64d7918034))
- **(backend)** Init all modules at boot for hot-reload route gating ([f46ddc2](https://github.com/orkestra-cc/orkestra/commit/f46ddc2534d48fa53c7b09122a88aae112a31811))
- **(backend)** Navigation filtering + admin API error handling ([adfaff3](https://github.com/orkestra-cc/orkestra/commit/adfaff304c095c522f094a60f4bfc5bcd3f14037))
- **(docker)** Add CORS_ORIGINS env var to backend service ([952d9ec](https://github.com/orkestra-cc/orkestra/commit/952d9ec4e5814056ab4def3e69012623ba6817ee))
- **(rag)** Ensure balanced document representation in scoped queries ([62b1003](https://github.com/orkestra-cc/orkestra/commit/62b10039f34509d3b15b04b85b070e36af12cd4b))
- **(agents)** Fix document checkbox not updating in manage modal ([480f2cf](https://github.com/orkestra-cc/orkestra/commit/480f2cf307c39e119f22a1c3292e576f1b8bff5e))
- **(graph)** Auto-refresh schema sidebar after document changes ([612d82e](https://github.com/orkestra-cc/orkestra/commit/612d82e4a53389eea00d72ef5828b95f300be195))
- **(graph)** Restore fcose layout and fix schema label browsing ([72b109b](https://github.com/orkestra-cc/orkestra/commit/72b109b735adce06db74e8e5c89427d51dbadb54))
- **(company)** Harden IT-search against missing data and timeouts ([52a99b4](https://github.com/orkestra-cc/orkestra/commit/52a99b4dbbfe462fd49e6c42a9a2441ed3ebb438))
- **(company)** Replace json.RawMessage with typed structs in enrichments ([4a3ec65](https://github.com/orkestra-cc/orkestra/commit/4a3ec65c5d206a8a18e60bf0012b8f7d6dbb0e7d))
- **(billing)** Update invoice status during sync when marking changes ([0580807](https://github.com/orkestra-cc/orkestra/commit/0580807cc9511dddc92c03b05a94b610f6f58b5e))
- **(billing)** Handle delivered marking and update status during sync ([6ea7fc8](https://github.com/orkestra-cc/orkestra/commit/6ea7fc8e9386619123724d83ee80bb82b4895a37))
- **(billing)** Always generate invoice PDFs locally with Gotenberg ([52b4e63](https://github.com/orkestra-cc/orkestra/commit/52b4e636535afca9a4505aa9fdc700b026c40c66))
- **(billing)** Add fallback deduplication by invoice number in SDI polling ([cd19d94](https://github.com/orkestra-cc/orkestra/commit/cd19d94b6e7284c37d14a7577e70302dc0f506fb))
- **(billing)** Subtract credit note amounts in invoice statistics ([e6c72cc](https://github.com/orkestra-cc/orkestra/commit/e6c72cc2634621763bc77ce963fbfdd6f6c5721c))
- **(billing)** Handle OpenAPI SDI error 612 with user-friendly message ([81c136b](https://github.com/orkestra-cc/orkestra/commit/81c136b142d9f8c2de1893d06718767d33192f7c))
- **(billing)** Use CodiceFiscale for IdTrasmittente in FatturaPA XML ([f39bbbb](https://github.com/orkestra-cc/orkestra/commit/f39bbbb7cb7f87ea65cd30ee3a64435109982de8))
- **(billing)** Correct FatturaPA XML download filename format ([89b2f08](https://github.com/orkestra-cc/orkestra/commit/89b2f087cb1a1d290c3f184821ec57f999168201))
- **(frontend)** Correct AltriDatiGestionali colspan and fix Vite HMR for staging ([ee637c8](https://github.com/orkestra-cc/orkestra/commit/ee637c87e6c15c22abccb0c2b383d0e0ec70bd10))
- **(billing)** Hide preview button for invoices not sent to SDI ([59f23ab](https://github.com/orkestra-cc/orkestra/commit/59f23abd30e6da5f5f6d2e6ddb8612a388067daf))
- **(billing)** Add xmlns attribute and CodiceFiscale validation fallback ([764245f](https://github.com/orkestra-cc/orkestra/commit/764245fab38243b37c938ce192e9a6705fc70a1b))
- **(billing)** Require all REA fields before generating IscrizioneREA element ([384ae39](https://github.com/orkestra-cc/orkestra/commit/384ae399be4cb09511acbe0f12ae2cc532037756))
- **(billing)** Add CodiceFiscale fallback to P.IVA for Italian companies ([11cac00](https://github.com/orkestra-cc/orkestra/commit/11cac00cdfe089250764bcba028d10c1c93c4bb3))
- **(documents)** Remove duplicate TOTALE in invoice PDF and improve template handling ([2e93fb7](https://github.com/orkestra-cc/orkestra/commit/2e93fb7f912abe916bd1da42ed24774568ec4cfa))
- **(docker)** Add CORS_ORIGINS config and improve healthchecks ([ff73f61](https://github.com/orkestra-cc/orkestra/commit/ff73f6189abdb97b9576543aa4c22b6838310954))
- **(frontend)** Resolve TypeScript errors and missing VITE_PUBLIC_URL ([e0b2f5c](https://github.com/orkestra-cc/orkestra/commit/e0b2f5c2676798ec94102052eda881d68666b8f7))
- **(docker)** Add missing OpenAPI env vars and fix rate limit naming ([ebb2ca5](https://github.com/orkestra-cc/orkestra/commit/ebb2ca56b42b97ec53a0fc05678b7b8e8a7c4ba6))
- **(frontend)** Resolve TypeScript build errors for staging deployment ([c920544](https://github.com/orkestra-cc/orkestra/commit/c920544606e93fbc2c573d7a0335c4f9f804b261))
- **(security)** Implement backend security audit recommendations ([3a4f0c1](https://github.com/orkestra-cc/orkestra/commit/3a4f0c155aa0f69f68c14e20dbe326885bc3c07e))
- **(billing)** Handle null invoice arrays in IssuedInvoiceDetail ([94b44d4](https://github.com/orkestra-cc/orkestra/commit/94b44d4a1f77efc2532d0e0789dfc7e9241d24ca))
- **(billing)** Add received invoice detail page to fix 404 error ([20873f4](https://github.com/orkestra-cc/orkestra/commit/20873f44cfefab96f3b83ec0fe26d1db71315789))
- **(documents)** Fix RTK Query transformResponse for Huma v2 API responses ([0d34975](https://github.com/orkestra-cc/orkestra/commit/0d34975e90dc02f3333e976e3de0145002f9fdab))
- **(dev)** Fix dev token 401 error and Huma pointer panic ([c5fb2ef](https://github.com/orkestra-cc/orkestra/commit/c5fb2ef6138b7a5e53b4b9a6cf4ae780f00e96f0))
- **(docker)** Use hardcoded internal ports in compose files ([cc0ba2d](https://github.com/orkestra-cc/orkestra/commit/cc0ba2d389c1ccc4b89610d3b53727e58e10b41d))
- **(billing)** Fix FatturaPA XML export validation errors ([ba0fa53](https://github.com/orkestra-cc/orkestra/commit/ba0fa539e87f7613e8e54ec66b4998b303f01a39))
- **(billing)** Add datiBollo field to invoice DTOs ([7d11a7e](https://github.com/orkestra-cc/orkestra/commit/7d11a7e2ca21b574323207b9729b875024f75b24))
- **(billing)** Fix invoice XML/HTML endpoints and FatturaPA compliance ([4789638](https://github.com/orkestra-cc/orkestra/commit/478963816b642e0565e83f2a57439534a0d3451c))
- **(billing)** Convert dates to RFC 3339 format for invoice API ([5b29d16](https://github.com/orkestra-cc/orkestra/commit/5b29d16f2282919d81f811120804e436fe239851))
- **(billing)** Handle null customers array in NewIssuedInvoice ([eadb32f](https://github.com/orkestra-cc/orkestra/commit/eadb32f8f46b3c4809107cfe2f01b19317ae20c5))
- **(billing)** Fix ECharts and table hooks in billing module ([cc54f78](https://github.com/orkestra-cc/orkestra/commit/cc54f78ea90bc023d24e64aaa1a18a33170dfff2))
- **(billing)** Resolve TypeScript errors in billing frontend module ([6b27a52](https://github.com/orkestra-cc/orkestra/commit/6b27a5299c3fc3a896242cd413d4f740b09b31b2))
- **(frontend)** Remove platform-specific rollup dependency ([c3c5e0c](https://github.com/orkestra-cc/orkestra/commit/c3c5e0c50b05236cb265aa7bab20729837b1219c))
- **(frontend,backend)** Fix broken sidebar navigation links ([6aec437](https://github.com/orkestra-cc/orkestra/commit/6aec437ef086279ef7069c57b8de166c22ac6d93))
- **(frontend)** Normalize Italian backend roles to English frontend roles ([22faf29](https://github.com/orkestra-cc/orkestra/commit/22faf29076ab8d3b090a43507189b45c833e2473))
- **(security)** Address audit findings for auth and input validation ([2464c80](https://github.com/orkestra-cc/orkestra/commit/2464c803191d3e6a3548310b98894a86eefacecf))
- **(deploy)** Auto-create Docker network if missing ([c6ec350](https://github.com/orkestra-cc/orkestra/commit/c6ec350bee4e14956e0418449f2256b54bbdb5a3))
- **(docker)** Add nginx.conf for staging/production frontend ([a23468d](https://github.com/orkestra-cc/orkestra/commit/a23468d01c969f5a452af7cb441dc9271e25a914))
- **(frontend)** Resolve TypeScript errors for production build ([d0a3c3d](https://github.com/orkestra-cc/orkestra/commit/d0a3c3d39161b1cf6aebf777ad1357c621669817))

### Style

- **(frontend-admin)** Fix prettier formatting ([1e88786](https://github.com/orkestra-cc/orkestra/commit/1e88786b4e1a1d3edd1a8d6b81e55f0d5e726bad))
- **(billing)** Swap invoice trend chart bar colors ([70c94a8](https://github.com/orkestra-cc/orkestra/commit/70c94a8a4817c1169aca0f743069f9f3c108c429))

### Refactor

- **(frontend-admin)** Empty notifications dropdown until real API ([023738b](https://github.com/orkestra-cc/orkestra/commit/023738bec3d2e68db2de80113e4b5f034833f9ea))
- **(frontend-admin)** Fold workspace + impersonate switchers into nine-dots menu ([26b2ea3](https://github.com/orkestra-cc/orkestra/commit/26b2ea3cf30066365d2330687ba29498945d6654))
- **(rag)** Carve addon into its own Go module (Phase 5l) ([f9a73c5](https://github.com/orkestra-cc/orkestra/commit/f9a73c52f88267324c29504bd503eef29ede3355))
- **(identity)** Carve addon into its own Go module (Phase 5k) ([944dd19](https://github.com/orkestra-cc/orkestra/commit/944dd19d914550783e60b99f9c6d098c75c62a4e))
- **(compliance)** Carve addon into its own Go module (Phase 5j) ([c9110b3](https://github.com/orkestra-cc/orkestra/commit/c9110b34ac5f13e3d8fd0295da94eb36fa411763))
- **(dev)** Carve addon into its own Go module (Phase 5i) ([55c7486](https://github.com/orkestra-cc/orkestra/commit/55c74867ea2cb79c85ae84b7bd431da3abc8b743))
- **(billing)** Carve addon into its own Go module (Phase 5h) ([be7d96b](https://github.com/orkestra-cc/orkestra/commit/be7d96b986a179d4706b019633cc4b6a4f1d5d75))
- **(payments)** Carve addon into its own Go module (Phase 5g) ([513b85d](https://github.com/orkestra-cc/orkestra/commit/513b85d0ab24457d3d0b9e59b7c40197eb88d8cb))
- **(subscriptions)** Carve addon into its own Go module (Phase 5f) ([91dbb94](https://github.com/orkestra-cc/orkestra/commit/91dbb9424545bf518dc64e445264bd342760364b))
- **(sales)** Carve addon into its own Go module (Phase 5e) ([012ad30](https://github.com/orkestra-cc/orkestra/commit/012ad30580288439d0142d2bff20693b8a53c5ac))
- **(graph)** Carve addon into its own Go module (Phase 5d) ([270170e](https://github.com/orkestra-cc/orkestra/commit/270170e13eb0789086e179c5977d61b43fe94f35))
- **(company)** Carve addon into its own Go module + extract shared openapiauth (Phase 5c) ([ad3be82](https://github.com/orkestra-cc/orkestra/commit/ad3be82720c6c6128df088e6932ca987b36f5dec))
- **(aimodels)** Carve addon into its own Go module (Phase 5b) ([3b751d1](https://github.com/orkestra-cc/orkestra/commit/3b751d1a48549ee8ef844b757b7c20502a981ee5))
- **(documents)** Carve addon into its own Go module (Phase 5a) ([fd664b6](https://github.com/orkestra-cc/orkestra/commit/fd664b6d7ca4e0f1bde5555fb270a3ec4fd6117a))
- **(auth)** Retire Dependencies.Config from the SDK (Phase 1c) ([b36e140](https://github.com/orkestra-cc/orkestra/commit/b36e14080d32658f74e593661fee5a1eaff27f70))
- **(sdk)** Consume published orkestra-sdk v0.1.0 ([56195c4](https://github.com/orkestra-cc/orkestra/commit/56195c4c28833441892e2a624cce6b46e4d81514))
- **(sdk)** Carve pkg/sdk/ into its own Go module + go.work ([a2fd7eb](https://github.com/orkestra-cc/orkestra/commit/a2fd7eb0953d54819e5d3a597a54b0f348c3ac4f))
- **(sdk)** Move User domain types to iface — pkg/sdk fully self-contained ([12195dd](https://github.com/orkestra-cc/orkestra/commit/12195ddbee7355bffe9b28110df9272b4aef97a7))
- **(sdk)** Cut 4 of 5 SDK→internal back-references ([dd9edd8](https://github.com/orkestra-cc/orkestra/commit/dd9edd8c6851c267c6e6451fa7dffb8f150ddeb7))
- **(sdk)** Split shared/middleware into ctxauth + modulegate ([ac32584](https://github.com/orkestra-cc/orkestra/commit/ac32584597ced1bab2adf40ef798301152916d6f))
- **(sdk)** Move SDK packages from internal/shared/ to pkg/sdk/ ([9d665c3](https://github.com/orkestra-cc/orkestra/commit/9d665c3c749a2e757e309a491b1f957a17560315))
- **(module)** Split Module interface into minimal + optional caps ([6aa95a9](https://github.com/orkestra-cc/orkestra/commit/6aa95a941cf06105a89641b0120d8585819d1e98))
- **(module)** Retire addon coupling to *config.Config ([6c13c76](https://github.com/orkestra-cc/orkestra/commit/6c13c7621caef4e03cafc4ce8cbe073b6deffc93))
- **(billing)** Consume config via UnmarshalModule ([9887fc7](https://github.com/orkestra-cc/orkestra/commit/9887fc71cfc5eab66507f3122f2776b163b7fe8c))
- **(rag)** Declare ConfigSchema + consume via UnmarshalModule ([3d24cab](https://github.com/orkestra-cc/orkestra/commit/3d24cabbced9d95f1bf214cc728fa64860a95c9b))
- **(agents)** Consume config via UnmarshalModule ([ecd469f](https://github.com/orkestra-cc/orkestra/commit/ecd469fb6af8e47307fd3f4d8dd3cc8562c6da37))
- **(sales)** Declare ConfigSchema + consume via UnmarshalModule ([4022787](https://github.com/orkestra-cc/orkestra/commit/4022787509e27a5ca6520b85f8da84951fbe83be))
- **(aimodels)** Consume config via UnmarshalModule ([03006b4](https://github.com/orkestra-cc/orkestra/commit/03006b46a5c5c9eac0623e43d69d5565c7094408))
- **(graph)** Consume config via UnmarshalModule ([4fe69f1](https://github.com/orkestra-cc/orkestra/commit/4fe69f1b6190e3e058608c7590e30198bd3df327))
- **(company)** Consume config via UnmarshalModule ([54774a8](https://github.com/orkestra-cc/orkestra/commit/54774a81d0828a1a7a2dee930e2bc8987d7a01b0))
- **(payments)** Consume config via UnmarshalModule ([b4a9fda](https://github.com/orkestra-cc/orkestra/commit/b4a9fdaad36b6350b612ec907286474d83a1af3e))
- **(documents)** Consume config via UnmarshalModule ([ad7e12d](https://github.com/orkestra-cc/orkestra/commit/ad7e12d8aa5a7040a0e9d20e85e5fdb05d342378))
- **(subscriptions)** Consume config via UnmarshalModule ([8230794](https://github.com/orkestra-cc/orkestra/commit/82307940a3dcb7053f41df708b0ab84717218e8a))
- **(user)** Drop legacy fleet-management cards and surface ([f0eb75a](https://github.com/orkestra-cc/orkestra/commit/f0eb75a5dbe81d5e62767a1de7a88a8826914e48))
- **(iface)** Own sales-side aimodels batch + usage types ([422920c](https://github.com/orkestra-cc/orkestra/commit/422920c00593c89590c8f74a5555820effe15089))
- **(iface)** Own aimodels contract types in iface package ([6d66145](https://github.com/orkestra-cc/orkestra/commit/6d661454c94f7101be1eeacb7ac4d01e58c30005))
- **(iface)** Own rag contract types in iface package ([c376988](https://github.com/orkestra-cc/orkestra/commit/c376988189f62fe5fdd97b2f56c4bfd532740dac))
- **(iface)** Own document contract types in iface package ([32b4389](https://github.com/orkestra-cc/orkestra/commit/32b4389ba908d08d865d59621707782f6d1c344f))
- **(iface)** Own graph contract types in iface package ([ad51e20](https://github.com/orkestra-cc/orkestra/commit/ad51e204135422b5ac0497c6eb15f2ff28e018d5))
- **(nav)** Admin realm + flatten sections ([497b5ba](https://github.com/orkestra-cc/orkestra/commit/497b5ba220be158a36e6028ee14f1a67796b9937))
- **(auth)** Retire onboarding addon ([b915509](https://github.com/orkestra-cc/orkestra/commit/b915509b02b2cb72de9517974ca847687276669d))
- **(core)** Polymorphic owner for billing surface ([5cfaa8e](https://github.com/orkestra-cc/orkestra/commit/5cfaa8e8abe9fa2ebc69066fbbab1ef963a01514))
- **(frontend-admin)** Rename frontend/ to frontend-admin/ ([e2b7df1](https://github.com/orkestra-cc/orkestra/commit/e2b7df1fd904b8ca88c8d6aaffa2b513f245e086))
- Per-audience APISurface (ADR-0003 PR-A) ([ed7af67](https://github.com/orkestra-cc/orkestra/commit/ed7af677f8e917961ce4273b5ec8030fdd50a9a9))
- **(nav)** Migrate all modules to Realm/Section/Tier ([e13380c](https://github.com/orkestra-cc/orkestra/commit/e13380c01ae90225eb1d9dee628c05b0efaf2fc5))
- **(frontend)** Phase 0.1 — align with backend Org→Tenant rename ([3616078](https://github.com/orkestra-cc/orkestra/commit/3616078be084888a308c46338826faa9254505f2))
- **(tenancy)** Phase 0.1 — full Org→Tenant backend rename (ADR-0001) ([f564dc8](https://github.com/orkestra-cc/orkestra/commit/f564dc82a6a24b046010c1751abd6a8aa52a94ce))
- **(frontend)** Modular route system with manifest pattern ([7a2a70c](https://github.com/orkestra-cc/orkestra/commit/7a2a70cf66d88643e1cf451488ac100c31fe954e))
- **(frontend)** Relocate addon admin pages to module dirs ([c57b600](https://github.com/orkestra-cc/orkestra/commit/c57b60025863c4c71f24577efc91a249a2479532))
- **(backend)** Finish multi-collection prefix rule ([28e168a](https://github.com/orkestra-cc/orkestra/commit/28e168a166d18a878c3e5d32606d6f112fb3608d))
- **(backend)** Prefix multi-collection module collections ([742f6a7](https://github.com/orkestra-cc/orkestra/commit/742f6a72544b125cb567cce3046a55d90d8f4fef))
- **(backend)** Plugin architecture with core/addons split ([48e8337](https://github.com/orkestra-cc/orkestra/commit/48e83371ff4b779b15e1b23511b92768bdd27cef))
- Remove unimplemented Reports & Deadlines module ([4387e29](https://github.com/orkestra-cc/orkestra/commit/4387e29ebdd874310f704d617e6edc0270988f34))
- **(backend)** Shared kernel interfaces for cross-module boundaries ([f25c23f](https://github.com/orkestra-cc/orkestra/commit/f25c23f6981af5127dfb47ff21108cbbad2bc90b))
- **(backend)** Remove hardcoded menu_config.go (-676 lines) ([016c189](https://github.com/orkestra-cc/orkestra/commit/016c1896fc867bef1dbab86ff25fcac1cfad55fb))
- **(backend)** Extract middleware, health, docs from main.go (Phase 3) ([f27a604](https://github.com/orkestra-cc/orkestra/commit/f27a60470058baee22c44168ee4e2ca60b2f7337))
- **(backend)** Migrate user, auth, dev — all 13 modules on registry ([2590e19](https://github.com/orkestra-cc/orkestra/commit/2590e193fa7febe1a90cb1a769b1288240758205))
- **(backend)** Migrate graph, rag, agents, sales to Module Registry ([cc9cc44](https://github.com/orkestra-cc/orkestra/commit/cc9cc44bae0a1d9bb46e8a6027eda0a4f6fad7da))
- **(backend)** Migrate billing module to Module Registry ([2e7b5bb](https://github.com/orkestra-cc/orkestra/commit/2e7b5bb06adf80321db2a9a1cc2a5ed9456d3e6c))
- **(backend)** Migrate documents and aimodels producer modules ([c9bcffb](https://github.com/orkestra-cc/orkestra/commit/c9bcffb391861176af59f1ec08805ffd0b006703))
- **(backend)** Migrate navigation, reporting, company to Module Registry ([4ee4b62](https://github.com/orkestra-cc/orkestra/commit/4ee4b6251e61ab019b41b56a8c112f471e4a19fb))
- **(backend)** Extract helpers from main.go into modules (Phase 0) ([de14dbb](https://github.com/orkestra-cc/orkestra/commit/de14dbb8e7d94b6526facc412549a89b8171f260))
- **(config)** Rename OpenAPI env vars for billing/company clarity ([c16a5c4](https://github.com/orkestra-cc/orkestra/commit/c16a5c41706bbe6a4e69efb9bd6d3d2211100ca4))
- **(api)** Remove /api prefix from all routes ([c785353](https://github.com/orkestra-cc/orkestra/commit/c7853530c51a090bf5e117e31efc8b976a2128b0))
- **(scripts)** Use single .env file with auto-detection ([a0b8535](https://github.com/orkestra-cc/orkestra/commit/a0b853500ebac2e7d6c1fe1a677936fd150314a7))
- **(docker)** Add project names and rename dev services ([2acc740](https://github.com/orkestra-cc/orkestra/commit/2acc740003d5201f745091c7fd5bcc74aa6ea150))
- **(frontend,backend)** Remove duplicate siteMaps and centralize navigation ([0e5e24e](https://github.com/orkestra-cc/orkestra/commit/0e5e24efad494fba6cdecce5b79f41a5a82e452c))
- **(frontend)** Consolidate Tables reference page and fix FeedCard error ([2539d4e](https://github.com/orkestra-cc/orkestra/commit/2539d4e4bf2421bd89f857fabacc55ec4b35deb1))
- **(frontend)** Implement /reference/* routes and fix admin routing ([0bf0f82](https://github.com/orkestra-cc/orkestra/commit/0bf0f82bdabde62f05463cb42460b8203802d5ba))
- **(frontend)** Consolidate reference materials into /reference directory ([ac6497c](https://github.com/orkestra-cc/orkestra/commit/ac6497c70140e8f126472400226df88893b1bc07))
- **(frontend)** Consolidate reference components into /reference directory ([293ca1e](https://github.com/orkestra-cc/orkestra/commit/293ca1e566d0a9011f64c3fc91532571d217516a))
- **(i18n)** Remove Italian language and switch to English ([3ae037d](https://github.com/orkestra-cc/orkestra/commit/3ae037df5d3dd2a95d44e38a71962dead47dc676))
- **(auth)** Standardize role names to English across backend ([62b1920](https://github.com/orkestra-cc/orkestra/commit/62b1920946429a7824c8e98af19bdcd436758f6d))

### Documentation

- **(site)** Deployment + onboarding guides (fork-readiness Phase 4) ([6f5e7ad](https://github.com/orkestra-cc/orkestra/commit/6f5e7ad406f78ba3b8d99e25b661a14cf95237d0))
- **(fork-readiness)** IT-addon callouts, GHCR override, OAuth guide promoted ([d266ce8](https://github.com/orkestra-cc/orkestra/commit/d266ce873eb3fd13ba196da22f04172610343a28))
- **(adr)** Replace {operator|client} path templates with {tier} in ADR-0003 ([3f61e10](https://github.com/orkestra-cc/orkestra/commit/3f61e1010ebaa3dc735d42dfa71ed40e11e07ac9))
- **(site)** Adopt monorepo as canonical source for docs.orkestra.cc ([537be8c](https://github.com/orkestra-cc/orkestra/commit/537be8c419eb381d63df2584252a8ace827b78eb))
- **(frontend-design-skill)** Rewrite to enforce reference-first UI work ([decbce2](https://github.com/orkestra-cc/orkestra/commit/decbce2157f9e93396850ee611e645130a51df36))
- **(marketing)** Add Phase 4 implementation plan ([ab6b4b8](https://github.com/orkestra-cc/orkestra/commit/ab6b4b88fc1bf4285b60db512862b3dbe15d9a50))
- **(marketing)** Add Phase 3 implementation plan ([6ade6a5](https://github.com/orkestra-cc/orkestra/commit/6ade6a52bd837424fe0699966294695c4b30b5a3))
- **(marketing)** Add Phase 2 implementation plan ([63a60cf](https://github.com/orkestra-cc/orkestra/commit/63a60cff0f33d76cde4fea649e3572cd7ca05f8a))
- **(i18n)** Plan status — Phase 4 partial completion 2026-05-20 ([6b365da](https://github.com/orkestra-cc/orkestra/commit/6b365dab41102d768640811aca27f6840d69bac5))
- **(i18n)** Plan frontend-admin EN+IT support + Phase 0 conventions ([5b47f78](https://github.com/orkestra-cc/orkestra/commit/5b47f787f75e5e1abab0cb7b491c6512dd67b404))
- **(marketing)** Refresh CLAUDE.md to reflect shipped Phase 1 (PR-6) ([1dc1e8b](https://github.com/orkestra-cc/orkestra/commit/1dc1e8bf462c58e51f686cf140b51777f1699496))
- **(adr-0004)** Brainstorm stock-media vendor APIs as out-of-scope exemplar ([82760e4](https://github.com/orkestra-cc/orkestra/commit/82760e43e4ac6b7d3ca3bfebb202483146e50930))
- **(adr-0004)** Add DataResidency axis + self-hosted fleet framing ([150a136](https://github.com/orkestra-cc/orkestra/commit/150a1365d752327f1d13dd3bd3660fe0ea1d9cf8))
- **(adr)** Publish ADRs 0001-0003 to docs.orkestra.cc ([50b8151](https://github.com/orkestra-cc/orkestra/commit/50b815189d859c5f0b8cecb6cbdddd57431038de))
- **(adr)** Add ADR-0004 external services integration framework ([356a958](https://github.com/orkestra-cc/orkestra/commit/356a958dd6d39cc24ab763f5da515d0adf40db0e))
- Refresh stale docs — archive abandoned plans, fix OAuth/env-var drift, sync status ([dba3106](https://github.com/orkestra-cc/orkestra/commit/dba31065f2694a374910d83bf5d57c1d833fb4e7))
- **(sdk)** Fix README hello-world example to compile against v0.2.0 ([4acf204](https://github.com/orkestra-cc/orkestra/commit/4acf204be26d392c1adaa454510bf416113f7e71))
- **(sdk)** Add public README and LICENSE for the standalone repo ([814d2b1](https://github.com/orkestra-cc/orkestra/commit/814d2b151373f313cb5a22da7de663f68120ce91))
- **(sdk)** Add SDK onboarding doc + pkg/sdk CLAUDE.md ([f0aa33a](https://github.com/orkestra-cc/orkestra/commit/f0aa33ad2b4f02ec66f42d28272c56e330bdd0df))
- **(plan)** Add Orkestra SDK split implementation plan ([5b12798](https://github.com/orkestra-cc/orkestra/commit/5b12798eae1e320eb50501cbb26c6bcaaaae4782))
- **(contributing)** Add explicit mise activation step ([5eb2159](https://github.com/orkestra-cc/orkestra/commit/5eb215981f517fe083be8ffbef177a0a9da16c12))
- **(readme)** Reframe pitch as SaaS-plumbing-already-done ([1e2a59c](https://github.com/orkestra-cc/orkestra/commit/1e2a59c320efd0044a1a71bb486496a85f1f3dbb))
- **(readme)** Drop em-dashes, fix CI badges, add coverage snapshot ([581950d](https://github.com/orkestra-cc/orkestra/commit/581950d17315f02134950acf063e5c33084d0337))
- **(readme)** Polish landing page with logo and badges ([0548e6f](https://github.com/orkestra-cc/orkestra/commit/0548e6f05c3c7576b31696dbe3db2985aeaeb0f8))
- **(onboarding)** Add tenancy model operator walkthrough ([3955f1f](https://github.com/orkestra-cc/orkestra/commit/3955f1fb0bc55c6fa99cd2066d9d84c0b76daf94))
- **(claude-md)** Align module counts and core load order with reality ([9a93356](https://github.com/orkestra-cc/orkestra/commit/9a93356bacf1745c25f5185c614c436ddae85dfd))
- **(orkestra-go-skill)** Rewrite against current architecture ([758b233](https://github.com/orkestra-cc/orkestra/commit/758b23329155c40a0985a6562636ad0daa2a40f5))
- **(frontend-client)** Add module CLAUDE.md ([9059134](https://github.com/orkestra-cc/orkestra/commit/90591345b0b2fc23fac51d1b782bc9753c715ca5))
- Rewrite Authentication_flow.md for the post-PR-D world ([300f2f1](https://github.com/orkestra-cc/orkestra/commit/300f2f13c45f089f84e74eed7efc19525d4e737b))
- Refresh ADR-0003 + auth/docker/scripts CLAUDE.md for D-9/D-10 ([17692ad](https://github.com/orkestra-cc/orkestra/commit/17692ad8745646b459be423e1ee7dce598a6af75))
- **(adr)** ADR-0003 — three-audience API host split (Proposed) ([bf79e41](https://github.com/orkestra-cc/orkestra/commit/bf79e4157de7680ed37db0ae217d5e1694164326))
- Document email/password auth and notification module ([33eddc5](https://github.com/orkestra-cc/orkestra/commit/33eddc5863654f9713f676942f249cb9e1a879c3))
- Document minimal profile in docker and backend CLAUDE.md ([53eb4bd](https://github.com/orkestra-cc/orkestra/commit/53eb4bd125f6c841dfa7f2a036b6dc03f2588191))
- Rewrite root and backend CLAUDE.md to match current architecture ([19dc95a](https://github.com/orkestra-cc/orkestra/commit/19dc95ac53cfd577fca5994345798428c9a2a942))
- Add architecture modernization report adapted to Orkestra ([4d5acd4](https://github.com/orkestra-cc/orkestra/commit/4d5acd451a2178c42b9bd0b670d64aa17f59012b))
- **(graph,rag)** Add module CLAUDE.md files ([a121ba8](https://github.com/orkestra-cc/orkestra/commit/a121ba804c8edb503950264a1b5f8d03ce9dc2ed))
- **(company)** Update CLAUDE.md with enrichment API and lookup flow ([ee543f1](https://github.com/orkestra-cc/orkestra/commit/ee543f1340936448ed7fe33618d6fddf3547a0bf))
- **(billing)** Add FatturaPA XML specification reference ([2d7360d](https://github.com/orkestra-cc/orkestra/commit/2d7360d6b639d93c24de607a81bf2bb4f37eed73))

### Tests

- **(frontend-admin)** En/it locale parity guardrail + plan status ([f5e1032](https://github.com/orkestra-cc/orkestra/commit/f5e103228ad21a9220df9e2fc45be77cd498a586))
- **(core)** Add unit tests for user + tenant handlers/services ([a39da63](https://github.com/orkestra-cc/orkestra/commit/a39da63d7114938c5f5315d14ef70c1c4f614026))
- **(sales)** Cover scorer, prompt loader, and report helpers ([ceb9167](https://github.com/orkestra-cc/orkestra/commit/ceb9167fd4b73b1348d22548c92fa8891e23dec3))
- **(billing)** Cover validation, config, and model helpers ([49124e0](https://github.com/orkestra-cc/orkestra/commit/49124e0aa32aff507f014bb97530a5f106d11027))
- **(notification)** Cover service layer with unit tests ([20c3c00](https://github.com/orkestra-cc/orkestra/commit/20c3c00cc657e989096810c6c49452931566ec57))
- Fix RTK 2.x typecheck and add mobile smoke test ([0da27c2](https://github.com/orkestra-cc/orkestra/commit/0da27c2658592d259d81ba6945c9d85ad5c0ee2e))
- **(frontend-admin)** MSW infra + regression suite for critical paths ([a239cea](https://github.com/orkestra-cc/orkestra/commit/a239cea49cfb072bc41ae70992283459aa6067bd))
- **(auth)** Drop unused mutex from orchOAuthRepo ([c421d40](https://github.com/orkestra-cc/orkestra/commit/c421d40c3fbfca2eeae0476d583a477188b90044))
- **(authz)** Cache-layer coverage with miniredis ([4f200e8](https://github.com/orkestra-cc/orkestra/commit/4f200e86a78510ea77cce8c46e1882e17936fe3f))
- **(auth)** Refresh-token rotation orchestration coverage ([ed0cab4](https://github.com/orkestra-cc/orkestra/commit/ed0cab40a036f88a46ac8d9645b038cd2cf7f76b))
- **(authz)** CRUD coverage for role + binding lifecycle ([beea5c8](https://github.com/orkestra-cc/orkestra/commit/beea5c871173a8daa05b96e257dba833893b7fd7))
- **(auth,middleware)** JWT helpers + RequireAuth integration coverage ([9b1b091](https://github.com/orkestra-cc/orkestra/commit/9b1b091781070a829d15aa9eb382219db2f07853))
- **(auth/handlers)** Cover error-mapping + small helper functions ([f9211d2](https://github.com/orkestra-cc/orkestra/commit/f9211d29452f2f44fd6f02acc242119743c61db6))
- **(authz,auth)** Tier-1 integration tests for permission gates ([b2ff4bd](https://github.com/orkestra-cc/orkestra/commit/b2ff4bdf4fb2c7ea1b472c821ac8535e80a4eec9))
- **(billing)** Align TestBuildMinimalB2BInvoice with actual XML form ([ad666b2](https://github.com/orkestra-cc/orkestra/commit/ad666b20e6fb05046e39fa7471df3e0489279813))
- **(auth)** Integration coverage for every policy gate ([bd064ac](https://github.com/orkestra-cc/orkestra/commit/bd064ac6830ec1a3c2681b6f5c0da2f12eb25d39))
- **(handlers)** Cover /v1/me/* polymorphic-owner fan-out ([b6d1c98](https://github.com/orkestra-cc/orkestra/commit/b6d1c98b70562449454c53a508d65393aef6d0d5))
- B-5 tier-guard repo tests (ADR-0003 PR-B) ([25fa506](https://github.com/orkestra-cc/orkestra/commit/25fa506afdd9519f23d217b7bc78171186b17496))

### Build

- **(makefile)** Add init-yes target for CI / scripted environments (fork-readiness Phase 7) ([3f09fe2](https://github.com/orkestra-cc/orkestra/commit/3f09fe260be011f4fbadc05f2063b68d7ff5c4e8))

### CI

- **(ai-service)** Build package, not single main.go file ([0d570ed](https://github.com/orkestra-cc/orkestra/commit/0d570eddfbff6a09ab2885d6c2ab9f89a22590cd))
- Scope mise installs per workflow to fix cache size ([b83e30f](https://github.com/orkestra-cc/orkestra/commit/b83e30fef86dbb1db2ddf657ebb4ffe495ab6e12))
- Thin GHA workflows over make targets ([d3e8623](https://github.com/orkestra-cc/orkestra/commit/d3e862381a30c9f74d9363d33b71c218cd0459e7))
- **(workflows)** Mirror govulncheck allowlist + allow manual mobile dispatch ([f5c5c14](https://github.com/orkestra-cc/orkestra/commit/f5c5c14748fddefa934bec1b111216d4822b7626))
- **(security)** Run security scan on push to dev and main ([3aea894](https://github.com/orkestra-cc/orkestra/commit/3aea894fe74c1bf75bf22e5cf08f242acaccd049))
- **(badges)** Auto-refresh coverage SVGs on push to dev/main ([e2bea1d](https://github.com/orkestra-cc/orkestra/commit/e2bea1d16fc4440c900abdf47051f2322bd39a0e))
- **(backend)** Get all four gate jobs green ([9ffee9d](https://github.com/orkestra-cc/orkestra/commit/9ffee9d91c064e6f29aff98dba174c603b6e5d45))
- **(docker)** Cache backend matrix builds via type=gha ([c7e5cea](https://github.com/orkestra-cc/orkestra/commit/c7e5ceaca8c9e5ae88e3ffcab94b4c969e966a69))
- **(docker)** Publish per-profile images to GHCR ([038aa26](https://github.com/orkestra-cc/orkestra/commit/038aa26dc0ecfedc0eb3dad926799a6966a42ad5))
- **(authz)** Gate Cedar policy coverage on permissions ([af0158e](https://github.com/orkestra-cc/orkestra/commit/af0158edf7406ef4630b922bd67f0286f09eb9e6))
- Add path-based GitHub Actions workflows for backend, frontend, and mobile ([d8ebd0a](https://github.com/orkestra-cc/orkestra/commit/d8ebd0a9fad9ba8d8c7b2cc8a8894ea7a94da62d))

### Dependencies

- **(deps)** Bump x/net to v0.55.0 across modules ([1d99832](https://github.com/orkestra-cc/orkestra/commit/1d9983219337dabd106889293d158e4bf1594345))
- **(deps)** Bump x/crypto to v0.52.0 across modules ([9e023c5](https://github.com/orkestra-cc/orkestra/commit/9e023c5a4c7f7a4232a2b0e517581c790f398aec))
- **(deps)** Bump orkestra-addon-identity to v0.1.1 ([f452410](https://github.com/orkestra-cc/orkestra/commit/f4524101cea30c0c7cdaf75391905b9c00b211cf))
- **(deps)** Bump orkestra-addon-{company,graph} to v0.1.1 ([4aebc88](https://github.com/orkestra-cc/orkestra/commit/4aebc881228410b0ce0852df63f0d2770321a726))
- **(deps)** Bump to Go 1.25.10 + x/net 0.54.0 for fresh stdlib advisories ([83284b8](https://github.com/orkestra-cc/orkestra/commit/83284b84e6135a20613fec6cc30b6b08575cf3c0))

### Chores

- **(governance)** CHANGELOG + GOVERNANCE + ROADMAP + release automation (fork-readiness Phase 6) ([29be6a9](https://github.com/orkestra-cc/orkestra/commit/29be6a9325926ed17a94cb49c5cd6b4c44040a58))
- **(frontend-admin)** Prettier autofix + plan status (Phase 4 deep tail) ([a10db11](https://github.com/orkestra-cc/orkestra/commit/a10db11225d13225f5d291a2fbe2657dd8a98bfa))
- **(frontend-admin)** Prettier autofix + plan status update (Phase 4 ✅) ([37a969a](https://github.com/orkestra-cc/orkestra/commit/37a969a6ab2620e6b6ed3774ba37392fbd180d6b))
- **(openapi)** Regenerate enterprise.json for Phase F log-level routes ([b2e24b8](https://github.com/orkestra-cc/orkestra/commit/b2e24b8f3a199c29e1b72b0ab4a3e354384e3bac))
- **(repo)** Retire the local-dev minimal stack ([210d4c0](https://github.com/orkestra-cc/orkestra/commit/210d4c0c5be1eb69c697908a5978d2a79b1f4a00))
- **(backend)** Quiet three boot-log noise sources ([6633736](https://github.com/orkestra-cc/orkestra/commit/6633736cf53bc198f0720d7ff023e00dba1d8c0b))
- **(ci)** Bump JS actions to Node 24 majors before June 2 deadline ([bae3013](https://github.com/orkestra-cc/orkestra/commit/bae3013e38aa2272df1d493e42b0d7842bfd1df4))
- **(frontend-admin)** Drop stale ESLint 9 migration comment ([9a893de](https://github.com/orkestra-cc/orkestra/commit/9a893de4bf4f40fd289ac18be59ddc6865084f12))
- **(identity)** Add go.sum + indirect block (Phase 5k v0.1.1 prep) ([97441b2](https://github.com/orkestra-cc/orkestra/commit/97441b2cdf4b6c2a580cae1bc74634e4d4a0e3d3))
- **(compliance)** Bump orkestra-sdk require to v0.3.0 ([70358f3](https://github.com/orkestra-cc/orkestra/commit/70358f34d31eef047784d47a7898a800986b6374))
- **(addons)** Tidy go.mod + ship go.sum for company and graph (Phase 5c/5d follow-up) ([f244658](https://github.com/orkestra-cc/orkestra/commit/f2446589fb64878e90a1d359574da22363e905ec))
- **(documents)** Bump orkestra-addon-documents to v0.1.0 ([05bb99a](https://github.com/orkestra-cc/orkestra/commit/05bb99a6382d407b283428cb94f4da1f3152d32d))
- **(tenantscope)** Drop 11 dangling internal/shared/module entries ([3029cd0](https://github.com/orkestra-cc/orkestra/commit/3029cd0995025bcf4e07db7e1b6aee13196dc6ff))
- **(sdk)** Bump orkestra-sdk require to v0.2.0 ([118ac8c](https://github.com/orkestra-cc/orkestra/commit/118ac8c487439c46ddeead4445057c7c3d4bd96d))
- **(middleware)** Tidy stale comment after ctxauth split ([f2d240e](https://github.com/orkestra-cc/orkestra/commit/f2d240eea31e29022ca68bf122a9b4c01073bcef))
- **(frontend-admin)** Drop dead webpack/CRA deps + wire postinstall ([7db5379](https://github.com/orkestra-cc/orkestra/commit/7db5379e4ec3bc1f1a4845f9e1ff47b020105ba4))
- **(frontend-admin)** Silence CI stderr + add coverage floor ([0f937a4](https://github.com/orkestra-cc/orkestra/commit/0f937a43e7ad90fc558b07b598542c00f9e4a5a1))
- **(make)** Wrap orkestra.sh + drop stale infra targets ([41f4d56](https://github.com/orkestra-cc/orkestra/commit/41f4d56e8f7c81a3f55b875a6ea0bccc8b3137ec))
- **(oss)** Add LICENSE, CoC, SECURITY, issue/PR templates ([1c55230](https://github.com/orkestra-cc/orkestra/commit/1c552302161722f7eff495e719cb4730cc14e88f))
- **(repo)** Add mise + Makefile + pre-commit tooling ([748a5cf](https://github.com/orkestra-cc/orkestra/commit/748a5cf4afa7b8bf34c9f5af935da4118baa6455))
- **(repo)** Normalize line endings to LF ([0bb7b6c](https://github.com/orkestra-cc/orkestra/commit/0bb7b6c72b68ecd071231a79c5826df9d3b68e11))
- **(frontend-admin)** Rebrand Falcon/Themewagon to Orkestra ([6f26adb](https://github.com/orkestra-cc/orkestra/commit/6f26adb32eb1b9c4f4de8b29a2e60cd20e82b682))
- **(frontend-admin)** Remove auth-test and role-navigation test pages ([eca2747](https://github.com/orkestra-cc/orkestra/commit/eca2747ddc780592e6955a8c0ce33d5ba087a7a5))
- **(frontend-admin)** Migrate to ESLint 9 flat config and clear npm audit ([500db53](https://github.com/orkestra-cc/orkestra/commit/500db5369d96296da80a44f0c4fc389e67042a0a))
- **(gitignore)** Untrack flutter-generated artifacts ([d1f203c](https://github.com/orkestra-cc/orkestra/commit/d1f203c7deb8c090a12f4a3c071075b37ffc5e94))
- **(mobile)** Bump locked deps to Flutter 3.41.9 ([be3a3c0](https://github.com/orkestra-cc/orkestra/commit/be3a3c03181222ca5ac7df5b5b08a3b3a6ccc521))
- **(commit-cmd)** Rewrite slash command around doc-hygiene priority ([12d5d8f](https://github.com/orkestra-cc/orkestra/commit/12d5d8fbd30ff5f0bbed6c588c6173f776677f0d))
- **(frontend-admin)** Drop chartjs/d3js refs, fix vite warnings ([2967758](https://github.com/orkestra-cc/orkestra/commit/2967758e608b1948a73babdd296dcf43b8a68ce1))
- **(docker)** Wire client SPA into staging + healthcheck ([ea40a1f](https://github.com/orkestra-cc/orkestra/commit/ea40a1fbfa0a1304532257693adaf09e0ec9b5a0))
- **(authz)** Drop stale subscriptions.client.manage example ([1ef6664](https://github.com/orkestra-cc/orkestra/commit/1ef66647b9104643c6b4ccf37a1826f6ee060d66))
- **(config)** Drop env vars now managed by ConfigService ([b39ce96](https://github.com/orkestra-cc/orkestra/commit/b39ce96438ba1ef775f30dfa9d9cbc221a2fe5df))
- **(nav)** Rename Operator realm label to Companies ([5fe0070](https://github.com/orkestra-cc/orkestra/commit/5fe0070389ba648a200082ba8247ab444ac4567d))
- Update gitignore and claude settings ([29573f7](https://github.com/orkestra-cc/orkestra/commit/29573f78b37ce70d6979ac4c23e1f549165c54d8))
- **(commands)** Tighten /commit slash command template ([5551c50](https://github.com/orkestra-cc/orkestra/commit/5551c504d623731e55d1ef396ea6f0aa7e75a49d))
- **(claude)** Replace generic Go agent with project-specific skills ([d830cf6](https://github.com/orkestra-cc/orkestra/commit/d830cf67f21621cbf3905c31557bc5d3d398fe41))
- **(frontend)** Update favicon configuration and remove health file ([6bed50a](https://github.com/orkestra-cc/orkestra/commit/6bed50aa484c5ee5515170b3fe28b5bccd05e7da))
- **(docker)** Add DNS servers to dev backend container ([a8208c5](https://github.com/orkestra-cc/orkestra/commit/a8208c503b052a486bb4bcf09176e878f24af982))
- Normalize line endings to LF ([fd1e9c8](https://github.com/orkestra-cc/orkestra/commit/fd1e9c8618ec79f36e682fbd9ff4204f0920b9e8))
- **(docker)** Add orkestra prefix to volume names ([7a27bd5](https://github.com/orkestra-cc/orkestra/commit/7a27bd5f3fdab60c0bdb60b3133f9d678321fae2))
- Sync codebase with latest development changes ([a4c7606](https://github.com/orkestra-cc/orkestra/commit/a4c76062d26b1e79f5ba916ba1536006d615d55d))
- Update Flutter tooling and local settings ([4a61f17](https://github.com/orkestra-cc/orkestra/commit/4a61f17fb98165c2ab838ba07a9479c05e183ee4))
- **(frontend)** Upgrade sass from 1.70.0 to 1.97.1 ([f799ee0](https://github.com/orkestra-cc/orkestra/commit/f799ee09178aee8c10bed42e8e5672c520ea86e5))
- **(frontend)** Upgrade npm packages to latest major versions ([8cb7102](https://github.com/orkestra-cc/orkestra/commit/8cb7102b46b9eaa84d946ba4f5f3b644744f3cc0))
- **(frontend)** Update npm packages and fix build errors ([15e5a5c](https://github.com/orkestra-cc/orkestra/commit/15e5a5c30f9e3518b6178f62830b2aacbc2915b0))
- Add claude settings and flutter widget preview scaffold ([fcbc41b](https://github.com/orkestra-cc/orkestra/commit/fcbc41bdb37e5270a15a1fbb2a881ac3a926f0ed))
- Remove unused orkestra images and add frontend-design skill ([9cc85b0](https://github.com/orkestra-cc/orkestra/commit/9cc85b02327e9dde4c8abbf025897f7771d62f2a))
- **(frontend)** Replace Sidereco assets with Orkestra branding ([f5a80d7](https://github.com/orkestra-cc/orkestra/commit/f5a80d72e267fda46eae48ba5abe129aecb1f9f9))
- **(claude)** Remove unused custom commands and add MCP config ([57368a1](https://github.com/orkestra-cc/orkestra/commit/57368a1ba92add8113c7e1fbc1156a8230484612))
- Rename ERP to Orkestra throughout codebase ([c43b0bd](https://github.com/orkestra-cc/orkestra/commit/c43b0bd57edabf3f77c7f2b2bb24d3f3cc3596b6))

<!-- generated by git-cliff -->
