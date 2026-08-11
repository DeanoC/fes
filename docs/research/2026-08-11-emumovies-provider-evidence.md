# Provider evidence for FogCast metadata: LaunchBox and EmuMovies

Date: 2026-08-11

This is technical compliance research, not legal advice. The evidence class is research only: no provider account login, authenticated API call, FTP connection, file redistribution, or hardware observation occurred. “Machine-observed” below means a bounded HTTPS retrieval or local parse performed for this report; it is not `Software-tested`, `Reproducible`, `HIL-observed`, or `Accepted` evidence for FogCast.

## Executive decision

**Conditional GO: use the public LaunchBox Games Database snapshot as FogCast's metadata and still-image provider.**
LaunchBox states that its database is crowd-sourced and provides game images and metadata across gaming platforms.[31][32]
Founder Jason Carr states that the downloadable dataset is updated daily, is how LaunchBox itself obtains the data for local processing, and that images are downloaded directly from the website.[29]
He later states that other applications already consume the database and that image metadata contains file names from which image URLs can be constructed.[28]

The current endpoint and data path were revalidated on 2026-08-11: anonymous HTTPS returned a 106,736,739-byte ZIP, and a direct image URL formed from a current archive file name returned JPEG bytes.[33][34] Local inspection found the fields needed by the existing FogCast presentation contract and exact platform names for SNES and Sega Genesis.

This is not an unlimited redistribution decision. No current public Terms of Use or CDN usage policy governing retention, attribution, request rate, mirroring, or redistribution was located. The current Games Database `robots.txt` explains that absent content signals neither grant nor restrict permission through that mechanism, and it contains no actual allow/deny signal.[36] Therefore the GO is limited to daily conditional snapshot refresh, local indexing, lazy user-requested image fetch, bounded local cache, and no bulk image mirroring or redistribution. A later contradictory term or direct vendor instruction must disable refresh and new image fetch fail closed.

**EmuMovies remains an optional future media supplement, not the primary metadata provider.** Its public material establishes member-gated API/Sync/FTP offerings, but current API documentation, current production permission, rate limits, and FTP transport details remain insufficient for FogCast integration.[1][9][22] No EmuMovies credentials or connection details were used.

## Research scope and provenance

| Item | Value |
| --- | --- |
| Repository worktree base | `701eb53cac0400deb772dfa4b1417bfd52df6516` |
| IGDB comparison commit | `298d7546a27f0941e9438571d9ff289562dcaff5` |
| First retrieval window | 2026-08-11 13:32–14:12 UTC |
| Evidence ledger | `/tmp/t_6f40d61b-citations/ledger.json` |
| Evidence ledger SHA-256 | `c502bd8b387f7afd00bc45e7c8f859d2e6374a598b64932b63cdfa44481b4c91` |
| First-party snapshots | `/tmp/t_6f40d61b-citations/evidence/` |
| Snapshot set | 34 files; sorted-manifest SHA-256 `e166f2e313081c759d1172fa526e080bd3c5f5e46c4ae7415ced876855ffb506` |
| LaunchBox archive | `/tmp/t_6f40d61b-citations/Metadata.zip` |
| LaunchBox inspection | `/tmp/t_6f40d61b-citations/launchbox-metadata-inspection.json` |
| External-source rule | Official provider sites, provider-hosted policies, and first-party provider forums only |
| Provider secret handling | No login, token, password, cookie, host-private endpoint, or private connection detail used or recorded |

The EmuMovies evidence set spans membership, terms, and privacy.[1][2][3]
Its specific API, FTP, Sync, platform, and asset evidence is cited where used.[9][11][23]
The LaunchBox evidence set includes the live database, product page, and founder forum statements.[28][29][31]
Current archive, image, and privacy observations are also retained.[33][34][35]
The current Games Database policy probe is recorded separately.[36]

### Machine-observed LaunchBox artifact manifest

| Observation | Result |
| --- | --- |
| `Metadata.zip` response | HTTPS 200; `application/x-zip-compressed`; 106,736,739 bytes |
| Archive validator metadata | ETag `"1dd2967869476e3"`; Last-Modified `Tue, 11 Aug 2026 08:00:41 GMT`; `Cache-Control: max-age=14400` |
| Archive SHA-256 | `627e9b0c55554ec232fc32ce50272b530dc5d30c7b179cbe09f7ec58b46925dd` |
| Archive members | `Metadata.xml`, `Mame.xml`, `Files.xml`, `Platforms.xml` |
| Largest member | `Metadata.xml`, 506,632,493 uncompressed bytes |
| Parsed records | 186,580 games; 1,316,025 game images; 189 platforms |
| Required platforms | exact `Super Nintendo Entertainment System` and `Sega Genesis` names present |
| Inspection JSON SHA-256 | `243cc4637ee5b66f7427116f7b80a509e81eca3b4e581eecc7ac1d43f786ceb0` |
| Direct image response | HTTPS 200; `image/jpeg`; 563×800 pixels |
| Direct image SHA-256 | `d5c7bd03ed34509e08942ee086800f0bd19d6cc1dd703120268f327ec0df0696` |
| Direct image integrity | archive `CRC32=1356669519`; fetched bytes `CRC32=1356669519` |
| Browser policy-link check | LaunchBox footer exposed Privacy Policy only; Games Database exposed no terms/privacy/license link |
| Guessed public policy routes | LaunchBox and Games Database terms/legal paths returned 404; `launchbox.gg` terms/privacy probes returned 522 |

The 2017 founder post described three XML files, while the 2026 archive contains four. This is direct evidence that implementation must inspect and version the live archive schema rather than freezing the historical forum description.[29][33]

## LaunchBox evidence and authority weighting

Jason Carr's statements are treated as affirmative first-party evidence of intended third-party application consumption, not merely anecdotal examples. In 2017 he said the whole dataset was downloadable, refreshed daily, used by LaunchBox itself for local processing, and paired with direct website image downloads.[29] In 2020 he said other applications already used the database and that the package's image file names could construct image URLs.[28]

Those statements support the provider GO, but they do not say that applications may mirror every image, redistribute cached files, omit attribution, retain data forever, or issue requests without limits. The current privacy policy also says the current policy applies each time the site is used, reinforcing the need to recheck provider policy at release and periodically thereafter.[35]

### Current LaunchBox capability matrix

| Capability | Current first-party or machine evidence | Confidence | FogCast disposition |
| --- | --- | --- | --- |
| Metadata transport | Founder-directed anonymous daily ZIP; live HTTPS 200 on 2026-08-11.[29][33] | High | GO with conditional GET and daily ceiling. |
| API | Historical thread discusses downloadable XML instead of scraping/API.[29] | High for snapshot path; no current API claim | Do not wait for or invent an API. |
| Third-party intent | Founder says other apps use the database and explains image URL construction.[28] | High | Supports application consumption. |
| Game fields | Machine-observed XML has name, overview, release date/year, genres, developer, publisher, max players, cooperative, platform, and database ID. | High for this archive | Maps to existing presentation fields. |
| Images | Founder says image file names construct URLs; live sample succeeded.[28][34] | High | Lazy fetch only. |
| Image metadata | Machine-observed `DatabaseID`, `FileName`, `Type`, `Region`, and `CRC32`. | High for this archive | Select deterministically and verify CRC32. |
| Image classes | Machine-observed front/back/3D box, fanart background, screenshots, logos, banners, discs, carts, and other types. | High for this archive | Initially expose only cover/backdrop. |
| Platform coverage | Live site states broad platform scope; current archive has 189 platform records.[31] | High for current snapshot | Exact mapping for the two FogCast systems only. |
| Authentication | Archive and sample image retrieved anonymously. | High for observed paths | No LaunchBox credentials or cookies in config. |
| Freshness | Founder says daily; current response has ETag, Last-Modified, and four-hour cache max-age.[29][33] | High | Conditional refresh no more than daily. |
| Rate limits | No first-party numeric image or archive limit located. | Unknown | Single snapshot refresh; conservative lazy image concurrency. |
| Attribution | No binding attribution wording located. | Unknown | Display `LaunchBox Games Database` for transparency; do not claim it satisfies a legal term. |
| Retention | No current public term located. | Unknown | Bounded replaceable cache; no indefinite archival mirror. |
| Redistribution | Founder evidence supports app use, not redistribution. | Unknown | Never serve provider media as a general public mirror or package it with releases. |
| Terms/CDN policy | Footer and likely policy paths yielded no current public Terms of Use; robots file has no actual content signal.[36] | Unknown | Record as a release gate, not a research blocker. |

### Archive field mapping to FogCast

| LaunchBox XML | FogCast internal field | Rule |
| --- | --- | --- |
| `DatabaseID` | `Candidate.ProviderID` | Decimal string, provider-scoped; cache key also includes archive validator/version. |
| `Name` | `Candidate.Name` | Required. Empty or malformed record is ignored. |
| no observed alternate-name field | `AlternativeNames` | Empty; do not infer aliases from website slugs. |
| exact platform name | `PlatformIDs` / `ProviderResult.PlatformID` | Use provider-local canonical name, never website route ID. |
| `Overview` | `Summary` | Trim only; do not rewrite or synthesize. |
| `ReleaseYear`, else year of `ReleaseDate` | `FirstReleaseYear` | Accept valid four-digit year; otherwise leave empty. |
| `Genres` | `Genres` | Parse only the delimiter observed in fixtures; trim, dedupe, preserve order. |
| `Developer`, then `Publisher` | `Studios` | Distinct non-empty values, developer first. |
| `MaxPlayers` | `Players` | Positive numeric text only; otherwise empty. |
| selected `GameImage` | `ArtworkRef` | Opaque file name plus expected CRC32; never expose upstream URL publicly. |
| archive ETag/Last-Modified plus record content | `UpdatedAt` / `Checksum` | Version provider data by snapshot, not wall-clock lookup time. |

### Deterministic platform and artwork policy

| FogCast identity | LaunchBox archive identity |
| --- | --- |
| `protocol.SystemSNES` | `Super Nintendo Entertainment System` |
| `protocol.SystemMegaDrive` | `Sega Genesis` |

| FogCast role | Ordered LaunchBox types | Region preference |
| --- | --- | --- |
| cover | `Box - Front`, then `Box - Front - Reconstructed`, then `Fanart - Box - Front` | caller region, then `World`, then empty, then deterministic lexical fallback |
| backdrop | `Fanart - Background`, then `Screenshot - Gameplay`, then `Screenshot - Game Title` | same ordering |

Only a file name present in the current parsed index is eligible. Build the upstream URL as HTTPS on the fixed `images.launchbox-app.com` authority with one escaped path segment; reject slashes, dot segments, controls, query text, and non-image extensions. Verify status, media type, existing byte/pixel limits, and the archive CRC32 before promotion into the artwork cache.

## EmuMovies evidence

EmuMovies membership classes distinguish basic artwork-only API/Sync access from supporting-member FTP, Sync, API, artwork, manuals, and video access.[1]
A developer-access post says the API was not open and required project details and direct instructions; a later answer described API access as beta and expected to become public.[9][10]
An August 2026 staff post says a new API was in beta and that the old Sync API had a failed content cache, so historical Sync behavior cannot establish a current server contract.[22]

The official Sync utility is a Windows application that automates matching, renaming, and media downloads.[7] That confirms user-facing functionality but not a reusable headless library or documented network protocol.

Supporting members are publicly described as having full FTP access, and the FAQ says connection details are account-bound.[1][5]
The subscriptions page separately lists supporter access to the FTP service.[17]
Public FTP material is internally inconsistent: an older FAQ says plain unencrypted FTP, while the current authenticated details route was inaccessible to this anonymous research.[18][27]
The operator's statement that the current logged-in page shows TLS is an operator observation, not accepted transport evidence.

Official category pages confirm SNES/Super Famicom and Sega Genesis/Mega Drive material.[11][12]
Representative pack pages confirm backgrounds and a Genesis video snap pack.[23][24][25]
The Sync FAQ notes that No-Intro naming changes over time and can create duplicates, so matching behavior must not be treated as a stable public identifier contract.[26]

EmuMovies terms prohibit posting copyrighted material and reposting downloaded content.[2] Accordingly FogCast must not bundle, redistribute, or expose a general-purpose proxy for EmuMovies media even if operator access is later granted.

### EmuMovies disposition matrix

| Route | Publicly evidenced behavior | Unknown or conflict | Decision |
| --- | --- | --- | --- |
| API | Membership benefit; developer access historically gated; current replacement in beta.[1][9][10][22] | current docs, endpoint, auth, IDs, limits, terms | WAIT for explicit access and docs. |
| Sync | Windows client automates matching/download.[7] | headless protocol, redistribution, lifecycle | REJECT as FogCast runtime dependency. |
| FTP | Supporting-member benefit; account-tied details.[1][5] | FTPS/SFTP mode, TLS minimum, cert behavior, limits, retention | WAIT; do not connect. |
| Website | Public categories and packs.[11][12][23][24][25] | stable machine contract | REJECT scraping. |
| Bulk media mirror | No public permission; reposting restriction.[2] | none needed | REJECT. |

Additional official FAQ, FTP-announcement, and Sync-category pages were retrieved to cross-check navigation and service descriptions.[4][6][8]
Representative SNES box, Genesis cart, and Mega Drive video pages were also checked directly.[13][14][15]
The SNES video page and focused member/connection FAQs corroborate the system coverage and account-tied, multi-connection service model.[16][19][20]
The focused Sync FAQ and a 2025 LaunchBox-hosted image-storage discussion were retained as secondary context, not as authority equal to current vendor policy or founder statements.[21][30]

Required personal action for an EmuMovies API path: submit the project through `https://emumovies.com/support/`, ask for current API documentation, app-scoped credentials, production permission, limits, cache/retention/attribution terms, and SNES plus Mega Drive identifiers.[9] Required personal action for an FTP path: while authenticated, inspect `https://emumovies.com/ftpdetails/` and report only non-secret protocol facts; never paste host, user name, password, token, cookie, or screenshot.[27]

## Comparison with the current IGDB baseline

The baseline at `298d7546a27f0941e9438571d9ff289562dcaff5` implements an opt-in IGDB provider behind `internal/metadata`, deterministic matching, provider-scoped SQLite cache, stale/negative behavior, bounded artwork fetching, host API projection, attribution, and purge-on-disable/removal.

| Dimension | IGDB baseline | LaunchBox decision | EmuMovies decision |
| --- | --- | --- | --- |
| Data path | credentialed request API | anonymous daily snapshot + local index | undocumented/gated API or member FTP |
| Metadata breadth | current presentation fields | current archive covers current fields | public evidence is media-first; descriptive fields unknown |
| Artwork | IGDB-specific URL constructor | provider-specific fixed-host constructor + CRC32 | unknown until docs/access |
| Freshness | request/cache TTL model | versioned snapshot, conditional daily refresh | unknown |
| Matching | provider candidates scored centrally | same central matcher over local candidates | retain same matcher if a future adapter exists |
| Credentials | private 0600 config | none | future app/FTP credentials only in private config |
| Availability | live API dependency | stale local index can continue | no accepted runtime path |
| Legal uncertainty | outside this report | intended app use evidenced; retention/rate/redistribution still open | reposting restriction and access ambiguity |

## Architecture implications for the next card

The public host/target protocol stays unchanged. Metadata remains host-only, optional presentation enrichment, and never changes launch identity, search membership, launch eligibility, request bodies, sessions, generations, or hardware ownership.

### Smallest coherent LaunchBox provider shape

| Area | Contract |
| --- | --- |
| Provider identity | Add `ProviderLaunchBox = "launchbox"`; retain `igdb` as rollback/comparison. |
| Config | `provider = "launchbox"` requires no client credentials; keep private-file validation for any credential-bearing provider. |
| Lookup | Implement `Provider.Lookup` against a provider-owned local index; return normal `ProviderResult` and let the existing matcher decide exact/confident/ambiguous/no-match. |
| Artwork resolution | Replace the IGDB-hardcoded URL builder with a provider-specific internal resolver. Add per-artwork integrity metadata rather than encoding URL/checksum into opaque text. |
| Network boundary | Hard-code HTTPS authorities for the archive and image host in the LaunchBox adapter; no operator-configurable URL and no redirects off allowlist. |
| Snapshot store | Provider-scoped root with archive, validator metadata, parsed index, and atomic current-generation pointer. Never write into ROM roots. |
| Startup | Open last validated index immediately. With no index, the first lookup performs one bounded singleflight bootstrap. |
| Refresh | At most once per 24 hours; use `If-None-Match` and `If-Modified-Since`; stale index remains readable while one refresh runs. |
| Ownership | Provider owns refresh context, temporary files, index handles, and optional goroutine; optional `Close()` cancels and joins before cache/root close. |
| Purge | Disable, provider switch, or `ErrProviderRemoved` purges LaunchBox metadata, archive, index, and artwork cache. |
| Attribution | Return provider `launchbox` and label `LaunchBox Games Database`; preserve current host API attribution boundary. |

### Snapshot safety contract

| Guard | Required behavior |
| --- | --- |
| Compressed size | Refuse over 256 MiB before download when length is known and while streaming regardless. |
| ZIP members | Maximum 8; reject absolute paths, `..`, directories, links, encryption, or unknown path structure. |
| Uncompressed size | Maximum 1 GiB total and 768 MiB for any member; enforce while streaming, not only from headers. |
| Compression ratio | Reject aggregate or member ratio above 25:1. |
| Required input | `Metadata.xml` and `Platforms.xml`; treat `Mame.xml` and `Files.xml` as unused until separately designed. |
| Parser | Streaming XML with explicit element/field limits; no DTD/entity/network resolution. |
| Validation | Require unique supported platform names, valid decimal database IDs, bounded strings, and known image records. |
| Promotion | Build a new index in a temporary provider-owned directory, fsync as applicable, validate counts/invariants, then atomically replace. |
| Failure | Keep last validated generation; with none, return `upstream_unavailable` or `invalid_response`; never serve a partial index. |
| Observability | Counts and age only; no ROM names, search text, provider URLs, file names, or remote response bodies. |

### Matching and cache semantics

| Topic | Required behavior |
| --- | --- |
| Query | Existing normalized ROM title plus protocol system; do not send ROM path/content upstream. |
| Candidate set | Exact current platform only; normalized exact names first; existing confidence/ambiguity thresholds remain authoritative. |
| Duplicate titles | Never pick by XML order. Preserve ambiguity unless existing deterministic score uniquely clears threshold. |
| Region | Use only for artwork ordering; do not mutate game identity from region. |
| Negative cache | Key by provider, platform-map policy, normalized title, region policy, and snapshot generation. |
| Positive cache | Include provider, database ID, candidate checksum, and snapshot generation so refresh invalidates stale facts deterministically. |
| Artwork cache | Bounded lazy cache; key includes provider, file name, expected CRC32, role, and transform policy. |
| Stale policy | Metadata may use last validated snapshot while refresh fails; a revoked/removed provider must purge rather than serve stale. |

### Failure and policy behavior

| Condition | Error/behavior |
| --- | --- |
| LaunchBox selected but bootstrap unavailable | `upstream_unavailable`; base catalog remains functional. |
| ZIP/XML violates limits or schema | `invalid_response`; preserve old validated generation. |
| Image filename/host/path violates policy | `policy_blocked`; no network request. |
| Image 404/5xx | existing upstream error mapping; base presentation remains usable. |
| Image CRC32 mismatch | `invalid_response`; discard temporary bytes and do not promote cache object. |
| Explicit later vendor prohibition | `provider_removed`; purge and disable the provider. |
| Unknown attribution/rate/retention term | conservative cache/rate defaults; release gate remains open. |
| EmuMovies transport or access unknown | do not connect; provider stays unconfigured. |

### Observability and privacy

| Safe | Forbidden |
| --- | --- |
| snapshot age/generation, conditional 304/200, parse result, record counts, cache hit/miss, categorized failures | archive/image URLs, ROM title/path, query text, provider image file name, IP/cookie/token/user name/password, response body |
| bounded aggregate download bytes and duration | raw XML fragments or image bytes in logs |
| provider name and public attribution label | credential-bearing config or browser session data |

## Release gates and unresolved risks

| Gate | Pass condition | Current state |
| --- | --- | --- |
| LaunchBox terms recheck | Current public terms/CDN policy located or vendor confirms app use, local cache, attribution, request limits, and retention | Open; no current terms located. |
| LaunchBox snapshot prototype | Fixture-driven parser/index, archive guards, exact SNES/Genesis mapping, deterministic match tests, atomic refresh tests | Not implemented. |
| LaunchBox artwork prototype | Fixed-host construction, redirect rejection, CRC32 check, current image byte/pixel limits, lazy cache tests | Not implemented. |
| LaunchBox provider lifecycle | cancellation, join, stale generation, provider switch, disable/purge, startup with/without index | Not implemented. |
| Independent review | Vega reviews exact diff and all Critical/Important findings are resolved | Pending downstream task. |
| EmuMovies access | explicit current docs/permission/limits or verified FTPS facts plus terms | Not established. |
| Physical behavior | separate HIL evidence if a later milestone makes hardware claims | Outside this report. |

## Recommendation

Proceed to architecture and implementation planning for a **LaunchBox snapshot provider** while preserving IGDB as comparison and rollback.
Treat founder statements as affirmative intended-use evidence.[28][29]
Treat the current anonymous archive/image retrieval as machine-observed feasibility evidence.[33][34]
Do not broaden that evidence into a redistribution, unlimited-request, or permanent-retention license.

Keep EmuMovies in the roadmap only as a later optional asset supplement after explicit current access and policy facts are obtained. Do not block the LaunchBox provider on EmuMovies, do not scrape either website, and do not change the public host/target protocol.

## Sources

[1] https://emumovies.com/member_classes — EmuMovies Member Classes
    > "Basic access to EmuMovies Sync and API (Artwork Only)"
    > "Access to EmuMovies Sync and API (Artwork, Manuals, Videos)"
[2] https://emumovies.com/terms — EmuMovies Registration Terms
    > "Please do not repost content downloaded from EmuMovies.com to other sites. Reposting content will lead to account termination."
[3] https://emumovies.com/privacy — EmuMovies Privacy Policy
    > "Log files are maintained and analysed of all requests for files on this website's web servers. Log files do not capture personal information but do capture the user's IP address, which is automatically recognised by our web servers."
[4] https://emumovies.com/faq — FAQ - EmuMovies
    > "Questions About the F.T.P."
[5] https://emumovies.com/faq/2-questions-about-the-ftp — Questions About the F.T.P.
    > "Your login information can be found by clicking the FTP Login link on the sites top menu."
    > "Both Sync and FTP use anywhere from 3-5 connections and it is suggested that you use a standard internet connection."
[6] https://emumovies.com/news/site-news/new-emumovies-ftp-is-now-live-r106 — New EmuMovies F.T.P. Is Now Live
    > "The EmuMovies F.T.P. File Server has moved to a new and MUCH MUCH improved server."
    > "Members who have F.T.P. access can click the F.T.P. Login link on the sites menu for access."
[7] https://emumovies.com/files/file/321-emumovies-sync — EmuMovies Sync 2.71
    > "The download service utility only downloads the content to match your roms and renames the content automatically to whatever romset you have so it just works!"
    > "Support for over 100 different systems"
    > "Minimum OS Requirement Win XP SP3 (Vista or Later Recommended)"
[8] https://emumovies.com/files/category/164-emumovies-sync — EmuMovies Sync
    > "Directly through your front-end or our app using EmuMovies Sync"
[9] https://emumovies.com/forums/topic/14973-where-is-the-api-documentation — Where is the API documentation?
    > "It's not an open API and we do have to grant you access and instructions."
[10] https://emumovies.com/forums/topic/70441-developer-access-request — Developer Access Request
    > "Submit a support ticket with the request, when our new api is out of beta we will open for public release and notify."
[11] https://emumovies.com/files/category/1195-nintendo-super-nintendo-super-famicom — Nintendo Super Nintendo / Super Famicom
    > "Nintendo Super Nintendo / Super Famicom"
[12] https://emumovies.com/files/category/1940-sega-genesis-mega-drive — Sega Genesis / Mega Drive
    > "Sega Genesis / Mega Drive"
[13] https://emumovies.com/files/file/1600-super-nintendo-entertainment-system-boxes-2d-pack-1885 — Super Nintendo Entertainment System Boxes - 2D Pack
    > "2D Boxes Pack for the Super Nintendo Entertainment System"
[14] https://emumovies.com/files/file/1189-sega-genesis-game-media-2d-pack-1215 — Sega Genesis Game Media - 2D Pack
    > "Game Media - 2D (Carts) for the Sega Genesis"
[15] https://emumovies.com/files/file/4094-sega-mega-drive-europe-video-snaps-pack-sqno-intro — Sega Mega Drive Europe Video Snaps Pack
    > "EmuMovies Official Video Snap Pack for the Sega Mega Drive"
[16] https://emumovies.com/files/file/4485-super-nintendo-video-snaps-no-intro-sq — Super Nintendo Video Snaps No-Intro
    > "EmuMovies Official Video Snap Collection for the Super Nintendo Entertainment System"
[17] https://emumovies.com/subscriptions — EmuMovies Donation Options
    > "Full access to the EmuMovies FTP"
[18] https://emumovies.com/faq/question/13-tls-negotiation-not-letting-me-log-in — TLS Negotiation not letting me log into FTP
    > "If you are seeing this error please set FTP type to "Plain FTP No Encryption""
[19] https://emumovies.com/faq/question/2-i-have-just-become-a-supporting-member — Supporting Member FTP Login
    > "Your login information can be found by clicking the FTP Login link on the sites top menu."
[20] https://emumovies.com/faq/question/12-i-am-having-trouble-connecting-to-ftp — Trouble Connecting to FTP
    > "Both Sync and FTP use anywhere from 3-5 connections and it is suggested that you use a standard internet connection."
[21] https://emumovies.com/faq/4-questions-about-emumovies-sync — Questions about EmuMovies Sync
    > "Questions about EmuMovies Sync"
[22] https://emumovies.com/forums/topic/70893-new-sync-tool-api-coming — New sync tool / api coming
    > "We have been beta testing the new one all year in HyperSpin 2, getting a new build of our new tool next week for testing."
    > "Oh that was a big whoops, that api is supposed to be auto building it's cache after the last update and it is not."
[23] https://emumovies.com/files/file/5635-super-nintendo-entertainment-system-backgrounds-pack-1544 — Super Nintendo Entertainment System Backgrounds Pack (1,544)
    > "Backgrounds Pack for the Super Nintendo Entertainment System"
    > "Availability: Site, FTP & Sync"
[24] https://emumovies.com/files/file/6162-sega-genesis-backgrounds-pack-1285 — Sega Genesis Backgrounds Pack (1,285)
    > "System & Game Backgrounds Pack for the Sega Genesis"
    > "Audited to No-Intro"
[25] https://emumovies.com/files/file/4093-sega-genesis-usa-video-snaps-pack-sqno-intro — Sega Genesis - USA Video Snaps Pack (SQ)(No-Intro)
    > "EmuMovies Official Video Snap Pack for the Sega Genesis"
    > "Availability - High Quality - FTP & Sync / Standard Quality - Site, FTP & Sync"
[26] https://emumovies.com/forums/topic/26971-what-naming-convention-does-emumovies-use — What naming convention does EmuMovies use?
    > "No intro does change slightly in some instances over time."
    > "Why don't you just use EM Sync so it 100% auto matches your rom name?"
[27] https://emumovies.com/ftpdetails — EmuMovies FTP details (authentication required)
    > "You do not have permission to view this page"
    > "Error code: 2T187/3"
[28] https://forums.launchbox-app.com/topic/54163-is-there-a-public-way-to-get-images-from-the-launchbox-games-database — Is there a public way to get images from the LaunchBox Games Database?
    > "Apparently there's some faulty info out there. There are at least a couple other apps using the LaunchBox Games Database already."
    > "This just simply isn't true lol. I don't know how you can look at the metadata package and conclude that, since it includes metadata for all of the images. It includes the image file names, which can be easily used to construct a URL. The IDs don't have to match up to the website URLs; they only have to match up to the rest of the metadata in the zip."
[29] https://forums.launchbox-app.com/topic/30123-launchbox-games-db-external-scraper — LaunchBox Games DB External Scraper
    > "This zip file is updated daily with all the latest metadata. This is exactly how LaunchBox itself ties into the data (it's much quicker than an API because all processing can happen locally). Inside of the zip file are the same three XML files that are found in the LaunchBox\Metadata folder. Images of course are downloaded directly from the website."
    > "Yeah, scraping should not at all be necessary since the entire data set is accessible and downloadable via XML. The way we do it via LaunchBox is prompt the user to optionally download a new set of data daily."
[30] https://forums.launchbox-app.com/topic/89957-finding-game-images-in-launchboxmetadatadb — Finding Game Images in LaunchBox.Metadata.db
    > "The [GUID] Image FileName shown is how it's stored on the Games Database website."
[31] https://gamesdb.launchbox-app.com — LaunchBox Games Database
    > "The LaunchBox Games Database aims to provide perfect game images and metadata for all known gaming platforms."
[32] https://www.launchbox-app.com — LaunchBox
    > "LaunchBox maintains its own crowd-sourced database for a massive number of games."
[33] https://gamesdb.launchbox-app.com/Metadata.zip — LaunchBox Games Database Metadata.zip
    > "content-type: application/x-zip-compressed content-length: 106736739"
[34] https://images.launchbox-app.com/1f763ad0-44cb-44e5-a22f-baa20f40e416.jpg — LaunchBox image asset sample
    > "date: Tue, 11 Aug 2026 14:08:34 GMT content-type: image/jpeg"
[35] https://www.launchbox-app.com/Resources/Documents/launchbox-privacy-policy.pdf — LaunchBox Privacy Policy
    > "Each time you use the Website, however, the current version of this Privacy Policy will apply."
[36] https://gamesdb.launchbox-app.com/robots.txt — LaunchBox Games Database robots.txt
    > "corresponding use, the website operator neither grants nor restricts"
