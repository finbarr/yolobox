import { defineConfig } from 'vitepress'

const SITE_URL = 'https://yolobox.dev'
const SITE_NAME = 'yolobox'
const SITE_DESCRIPTION = 'Run AI coding agents in a sandboxed container. Your home directory stays home.'
const SOCIAL_IMAGE_URL = `${SITE_URL}/social-card.png`
const SOCIAL_IMAGE_ALT = 'The yolobox logo centered on a black background.'

const pageSeo: Record<string, { title: string, description: string, noindex?: boolean }> = {
  '/': {
    title: 'Sandboxed AI Coding Agents in a Container',
    description: 'Run Claude, Codex, Gemini, Copilot, and other AI coding agents inside a sandboxed container with your project mounted at its real path and your home directory kept off-limits.',
  },
  '/getting-started': {
    title: 'Installation and Setup',
    description: 'Install yolobox with Homebrew or the install script, choose Docker or Podman, and launch your first sandboxed AI coding agent session.',
  },
  '/commands': {
    title: 'Commands Reference',
    description: 'Reference for yolobox commands, including AI shortcuts like yolobox claude, plus run, setup, config, upgrade, reset, and uninstall workflows.',
  },
  '/recipes': {
    title: 'Recipes',
    description: 'Common yolobox workflows, including named agent environments, Git remote synchronization, and forked webapp routing with local HTTPS.',
  },
  '/configuration': {
    title: 'Configuration Guide',
    description: 'Configure yolobox with global and project settings for mounts, environment variables, forwarded credentials, runtime behavior, and per-project customization.',
  },
  '/customizing': {
    title: 'Project-Level Customization',
    description: 'Customize yolobox per project with apt packages, Dockerfile fragments, derived images, and fully custom image workflows.',
  },
  '/flags': {
    title: 'CLI Flags Reference',
    description: 'Complete yolobox flag reference for runtime, filesystem, config, networking, resources, and image customization options.',
  },
  '/security': {
    title: 'Security Model',
    description: 'Understand the yolobox trust boundary, what the container sandbox protects, and where stronger isolation is still required.',
  },
  '/whats-in-the-box': {
    title: 'Included Tools and Base Image',
    description: 'See what ships in the yolobox base image, including AI CLIs, runtimes, build tools, utilities, and YOLO-mode wrappers.',
  },
  '/contributing': {
    title: 'Contributing Guide',
    description: 'Build, test, lint, and contribute to yolobox, including repo workflow expectations and docs-site development commands.',
  },
  '/404': {
    title: 'Page Not Found',
    description: 'The requested page could not be found on yolobox.dev.',
    noindex: true,
  },
}

function routeFromRelativePath(relativePath: string) {
  if (relativePath === 'index.md') {
    return '/'
  }

  return `/${relativePath.replace(/\.md$/, '')}`
}

function canonicalUrlForRoute(route: string) {
  if (route === '/') {
    return `${SITE_URL}/`
  }

  return `${SITE_URL}${route}`
}

function seoForPage(relativePath: string, fallbackTitle: string, fallbackDescription: string) {
  const route = routeFromRelativePath(relativePath)
  const override = pageSeo[route]

  return {
    route,
    title: override?.title ?? fallbackTitle,
    description: override?.description ?? (fallbackDescription || SITE_DESCRIPTION),
    canonicalUrl: canonicalUrlForRoute(route),
    noindex: override?.noindex ?? false,
  }
}

export default defineConfig({
  title: SITE_NAME,
  description: SITE_DESCRIPTION,
  sitemap: {
    hostname: SITE_URL,
  },

  vite: {
    server: {
      allowedHosts: ['localhost', 'host.docker.internal', 'yolobox-docs-dev'],
    },
  },

  head: [
    ['link', { rel: 'icon', href: '/favicon.svg' }],
  ],

  transformPageData(pageData) {
    const seo = seoForPage(
      pageData.relativePath,
      pageData.title || SITE_NAME,
      pageData.description || SITE_DESCRIPTION
    )

    return {
      title: seo.title,
      titleTemplate: `:title | ${SITE_NAME}`,
      description: seo.description,
    }
  },

  transformHead({ pageData, title, description }) {
    const seo = seoForPage(
      pageData.relativePath,
      pageData.title || SITE_NAME,
      pageData.description || SITE_DESCRIPTION
    )

    const head = [
      ['meta', { property: 'og:type', content: 'website' }],
      ['meta', { property: 'og:site_name', content: SITE_NAME }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:url', content: seo.canonicalUrl }],
      ['meta', { property: 'og:image', content: SOCIAL_IMAGE_URL }],
      ['meta', { property: 'og:image:secure_url', content: SOCIAL_IMAGE_URL }],
      ['meta', { property: 'og:image:type', content: 'image/png' }],
      ['meta', { property: 'og:image:width', content: '1200' }],
      ['meta', { property: 'og:image:height', content: '630' }],
      ['meta', { property: 'og:image:alt', content: SOCIAL_IMAGE_ALT }],
      ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
      ['meta', { name: 'twitter:url', content: seo.canonicalUrl }],
      ['meta', { name: 'twitter:domain', content: 'yolobox.dev' }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
      ['meta', { name: 'twitter:image', content: SOCIAL_IMAGE_URL }],
      ['meta', { name: 'twitter:image:alt', content: SOCIAL_IMAGE_ALT }],
    ]

    if (seo.noindex) {
      head.push(['meta', { name: 'robots', content: 'noindex, nofollow' }])
      return head
    }

    head.unshift(['link', { rel: 'canonical', href: seo.canonicalUrl }])
    return head
  },

  appearance: 'dark',
  cleanUrls: true,

  themeConfig: {
    siteTitle: 'yolobox',

    nav: [
      { text: 'Get Started', link: '/getting-started' },
      { text: 'Customize', link: '/customizing' },
      { text: 'Reference', link: '/flags' },
      { text: 'Security', link: '/security' },
    ],

    sidebar: [
      {
        text: 'Start Here',
        items: [
          { text: 'Overview', link: '/' },
          { text: 'Installation & Setup', link: '/getting-started' },
          { text: 'Commands', link: '/commands' },
          { text: 'Recipes', link: '/recipes' },
          { text: "What's in the Box", link: '/whats-in-the-box' },
        ]
      },
      {
        text: 'Customize & Configure',
        items: [
          { text: 'Project-Level Customization', link: '/customizing' },
          { text: 'Configuration', link: '/configuration' },
          { text: 'Flags', link: '/flags' },
        ]
      },
      {
        text: 'Safety & Project',
        items: [
          { text: 'Security Model', link: '/security' },
          { text: 'Contributing', link: '/contributing' },
          { text: 'Changelog', link: 'https://github.com/finbarr/yolobox/blob/master/CHANGELOG.md' },
        ]
      }
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/finbarr/yolobox' }
    ],

    editLink: {
      pattern: 'https://github.com/finbarr/yolobox/edit/master/docs/:path',
      text: 'Edit this page on GitHub'
    },

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright 2026 Finbarr Taylor'
    },

    search: {
      provider: 'local'
    },

    outline: {
      level: [2, 3],
      label: 'On this page'
    },
  }
})
