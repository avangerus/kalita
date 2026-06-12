# Content — an agent-native, headless CMS as a kalita pack.
#
# The thesis in one file: a business site is just governed content. Pages,
# services and articles are plain kalita entities; "publish" is a workflow move
# with human-in-the-loop, not a button buried in an admin panel. The owner never
# opens a CMS — they tell Claude what to change, Claude drafts it over MCP, and
# the owner SIGNS the go-live. Every edit is journaled and one-click reversible,
# which is the one thing Bitrix's content editing never gave you.
#
# What renders the public site is a separate front layer (served from --ui-dir):
# this pack owns the model and the governance, not the HTML.

entity SiteSettings singleton:
    site_name: string required default="My Site"
    domain: string
    tagline: string
    logo: file
    primary_color: string default="#0b5"
    footer: text

entity Page:
    slug: string required unique          # "/", "about", "contact" — the URL
    title: string required
    hero: string                          # big headline on the page
    body: text                            # markdown the renderer turns into HTML
    seo_description: string
    menu_order: int default=0             # 0 = not in the nav
    status: enum[Draft, Review, Published, Archived] default=Draft

entity Service:
    slug: string required unique
    name: string required
    summary: string                       # one-line pitch in the catalog
    body: text
    price_from: int                       # shown as "from N" — money is data, not a payment
    icon: string
    order: int default=0
    status: enum[Draft, Review, Published, Archived] default=Draft

entity Article:
    slug: string required unique
    title: string required
    excerpt: string
    body: text
    author: string
    published_at: datetime
    status: enum[Draft, Review, Published, Archived] default=Draft

# Publishing is the same governed shape for every content type: an agent (or
# editor) drafts and submits; going live REQUIRES the owner's approval. That HITL
# gate is the whole point — the agent can prepare everything, but a human signs
# the moment it becomes public.
workflow Page on status:
    Draft     -> Review:    submit_page
    Review    -> Published: publish_page requires approval(Owner)
    Review    -> Draft:     reject_page
    Published -> Draft:     unpublish_page
    any       -> Archived:  archive_page

workflow Service on status:
    Draft     -> Review:    submit_service
    Review    -> Published: publish_service requires approval(Owner)
    Review    -> Draft:     reject_service
    Published -> Draft:     unpublish_service
    any       -> Archived:  archive_service

workflow Article on status:
    Draft     -> Review:    submit_article
    Review    -> Published: publish_article requires approval(Owner)
    Review    -> Draft:     reject_article
    Published -> Draft:     unpublish_article
    any       -> Archived:  archive_article

roles:
    Owner
    Editor
    ContentBot agent          # Claude-over-MCP: drafts and submits, never self-publishes
    SiteBot agent             # the renderer: reads Published content, writes the public site

permissions:
    Owner:
        full    [SiteSettings, Page, Service, Article]
        act     [submit_page, reject_page, unpublish_page, archive_page, submit_service, reject_service, unpublish_service, archive_service, submit_article, reject_article, unpublish_article, archive_article]
        approve [publish_page, publish_service, publish_article]
    Editor:
        read   [SiteSettings]
        full   [Page, Service, Article]
        act    [submit_page, reject_page, unpublish_page, submit_service, reject_service, unpublish_service, submit_article, reject_article, unpublish_article]
    # the agent prepares content but the go-live gate is a human's: it may draft,
    # edit and submit for review, but publish needs the Owner's signature.
    ContentBot:
        read   [SiteSettings]
        create [Page, Service, Article]
        update [Page, Service, Article]
        act    [submit_page, submit_service, submit_article]
        deny   [delete *, update SiteSettings.*, act publish_page, act publish_service, act publish_article]
    # read-only by construction: it renders the site, it cannot change it
    SiteBot:
        read   [SiteSettings, Page, Service, Article]
        deny   [delete *, create *, update *]

automation:
    # rebuild the public site from Published content. Driving regen off a short
    # interval keeps the renderer dead simple — one rule, no per-entity fan-out —
    # and 2-minute freshness is fine for a content site. (The kernel now honors
    # `every <dur>` exactly; see engine/automation.go scheduleInterval.)
    on schedule every 2m:
        agent SiteBot: regen()

ui Page:
    list: [slug, title, status, menu_order] sort=menu_order
        filters: [status]
    board: by status

ui Service:
    list: [slug, name, price_from, status, order] sort=order
        filters: [status]
    board: by status

ui Article:
    list: [slug, title, author, status] sort=-published_at
        filters: [status]
    board: by status

dashboard ContentOverview "Site content":
    tile "Published pages":   count Page where status = Published
    tile "Pages in review":   count Page where status = Review
    tile "Published services": count Service where status = Published
    tile "Articles live":     count Article where status = Published
    tile "Pages by status":   count Page group by status
