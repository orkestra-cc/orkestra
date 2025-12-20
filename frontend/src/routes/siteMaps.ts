import paths, { rootPaths } from './paths';

export interface Badge {
  type: string;
  text: string;
}

export interface NavItem {
  name: string;
  to?: string;
  icon?: string | string[];
  active?: boolean;
  exact?: boolean;
  newtab?: boolean;
  badge?: Badge;
  label?: string;
  children?: NavItem[];
  roles?: string[]; // Required user roles to access this item
  permissions?: string[]; // Required permissions to access this item
}

export interface RouteGroup {
  label: string;
  labelDisable?: boolean;
  children: NavItem[];
  roles?: string[]; // Required user roles to access this entire group
  permissions?: string[]; // Required permissions to access this entire group
}

export const dashboardRoutes: RouteGroup = {
  label: 'Cruscotto',
  labelDisable: true,
  roles: ['developer'], // Tutti gli utenti autenticati possono accedere al cruscotto
  children: [
    {
      name: 'Cruscotto',
      active: true,
      icon: 'chart-pie',
      children: [
        {
          name: 'Predefinito',
          to: rootPaths.root,
          exact: true,
          active: true
        },
        {
          name: 'Analisi',
          to: paths.analytics,
          active: true,
          roles: ['developer'] // Analisi avanzate per gli amministratori
        },
        {
          name: 'CRM',
          to: paths.crm,
          active: true
        },
        {
          name: 'Gestione',
          to: paths.projectManagement,
          active: true,
          roles: ['developer'] // Gestione progetti per manager e superiori
        },
        {
          name: 'SaaS',
          to: paths.saas,
          active: true
        },
        {
          name: 'Supporto tecnico',
          to: paths.supportDesk,
          active: true
        }
      ]
    }
  ]
};
export const appRoutes: RouteGroup = {
  label: 'Applicazioni',
  roles: ['developer'], // Funzionalità di base per tutti gli utenti autorizzati
  children: [
    {
      name: 'Calendario',
      icon: 'calendar-alt',
      to: paths.calendar,
      active: true
    },
    {
      name: 'Chat',
      icon: 'comments',
      to: paths.chat,
      active: true
    },
    {
      name: 'Email',
      icon: 'envelope-open',
      active: true,
      children: [
        {
          name: 'Posta in arrivo',
          to: paths.emailInbox,
          active: true
        },
        {
          name: 'Dettaglio email',
          to: paths.emailDetail,
          active: true
        },
        {
          name: 'Componi',
          to: paths.emailCompose,
          active: true
        }
      ]
    },
    {
      name: 'Eventi',
      icon: 'calendar-day',
      active: true,
      children: [
        {
          name: 'Crea evento',
          to: paths.createEvent,
          active: true
        },
        {
          name: 'Dettaglio evento',
          to: paths.eventDetail,
          active: true
        },
        {
          name: 'Lista eventi',
          to: paths.eventList,
          active: true
        }
      ]
    },
    {
      name: 'Kanban',
      icon: ['fab', 'trello'],
      to: paths.kanban,
      active: true
    },
    {
      name: 'Social',
      icon: 'share-alt',
      active: true,
      children: [
        {
          name: 'Feed',
          to: paths.feed,
          active: true
        },
        {
          name: 'Registro attività',
          to: paths.activityLog,
          active: true
        },
        {
          name: 'Notifiche',
          to: paths.notifications,
          active: true
        },
        {
          name: 'Followers',
          to: paths.followers,
          active: true
        }
      ]
    },
    {
      name: 'Supporto tecnico',
      icon: 'ticket-alt',
      active: true,
      children: [
        {
          name: 'Vista tabella',
          to: paths.ticketsTable,
          active: true
        },
        {
          name: 'Vista scheda',
          to: paths.ticketsCard,
          active: true
        },
        {
          name: 'Contatti',
          to: paths.contacts,
          active: true
        },
        {
          name: 'Dettagli contatto',
          to: paths.contactDetails,
          active: true
        },
        {
          name: 'Anteprima ticket',
          to: paths.ticketsPreview,
          active: true
        },
        {
          name: 'Collegamenti rapidi',
          to: paths.quickLinks,
          active: true
        },
        {
          name: 'Report',
          to: paths.reports,
          active: true
        }
      ]
    }
  ]
};

export const pagesRoutes: RouteGroup = {
  label: 'Pagine',
  roles: ['developer'], // Gestione pagine per manager e superiori
  children: [
    {
      name: 'Avvio',
      icon: 'flag',
      to: paths.starter,
      active: true
    },
    {
      name: 'Landing',
      icon: 'globe',
      to: paths.landing,
      active: true
    },
    {
      name: 'Autenticazione',
      icon: 'lock',
      active: true,
      children: [
        {
          name: 'Accesso',
          to: paths.login,
          active: true
        }
      ]
    },
    {
      name: 'Utente',
      icon: 'user',
      active: true,
      roles: ['developer'], // Tutti gli utenti possono accedere al proprio profilo
      children: [
        {
          name: 'Profilo',
          to: paths.userProfile,
          active: true,
          roles: ['operator']
        },
        {
          name: 'Impostazioni',
          to: paths.userSettings,
          active: true,
          roles: ['operator']
        }
      ]
    },
    {
      name: 'Prezzi',
      icon: 'tags',
      active: true,
      children: [
        {
          name: 'Prezzi predefiniti',
          to: paths.pricingDefault,
          active: true
        },
        {
          name: 'Prezzi alternativi',
          to: paths.pricingAlt,
          active: true
        }
      ]
    },
    {
      name: 'FAQ',
      icon: 'question-circle',
      active: true,
      children: [
        {
          name: 'FAQ di base',
          to: paths.faqBasic,
          active: true
        },
        {
          name: 'FAQ alternative',
          to: paths.faqAlt,
          active: true
        },
        {
          name: 'FAQ a fisarmonica',
          to: paths.faqAccordion,
          active: true
        }
      ]
    },
    {
      name: 'Errori',
      active: true,
      icon: 'exclamation-triangle',
      children: [
        {
          name: '404',
          to: paths.error404,
          active: true
        },
        {
          name: '500',
          to: paths.error500,
          active: true
        }
      ]
    },
    {
      name: 'Varie',
      icon: 'thumbtack',
      active: true,
      children: [
        {
          name: 'Associazioni',
          to: paths.associations,
          active: true
        },
        {
          name: 'Invita persone',
          to: paths.invitePeople,
          active: true
        },
        {
          name: 'Privacy policy',
          to: paths.privacyPolicy,
          active: true
        }
      ]
    },
    {
      name: 'Layout',
      icon: 'columns',
      active: true,
      badge: {
        type: 'success',
        text: 'Nuovo'
      },
      children: [
        {
          name: 'Navbar verticale',
          to: paths.verticalNavLayout,
          active: true,
          newtab: true
        },
        {
          name: 'Nav superiore',
          to: paths.topNavLayout,
          active: true,
          newtab: true
        },
        {
          name: 'Doppio superiore',
          to: paths.doubleTopNavLayout,
          active: true,
          newtab: true
        },
        {
          name: 'Nav combinato',
          to: paths.comboNavLayout,
          active: true,
          newtab: true
        }
      ]
    }
  ]
};

export const modulesRoutes: RouteGroup = {
  label: 'Moduli',
  roles: ['developer'], // Amministratore e superiori per i moduli di sviluppo
  children: [
    {
      name: 'Forms',
      active: true,
      icon: 'file-alt',
      children: [
        {
          name: 'Basic',
          active: true,
          children: [
            {
              name: 'Form control',
              to: paths.formControl,
              active: true
            },
            {
              name: 'Input group',
              to: paths.inputGroup,
              active: true
            },
            {
              name: 'Select',
              to: paths.select,
              active: true
            },
            {
              name: 'Checks',
              to: paths.checks,
              active: true
            },
            {
              name: 'Range',
              to: paths.range,
              active: true
            },
            {
              name: 'Layout',
              to: paths.formLayout,
              active: true
            }
          ]
        },
        {
          name: 'Advance',
          active: true,
          children: [
            {
              name: 'Advance select',
              to: paths.advanceSelect,
              active: true
            },
            {
              name: 'Date picker',
              to: paths.datePicker,
              active: true
            },
            {
              name: 'Editor',
              to: paths.editor,
              active: true
            },
            {
              name: 'Emoji button',
              to: paths.emojiButton,
              active: true
            },
            {
              name: 'File uploader',
              to: paths.fileUploader,
              active: true
            },
            {
              name: 'Input mask',
              to: paths.inputMask,
              active: true
            },
            {
              name: 'Range slider',
              to: paths.rangeSlider,
              active: true
            },
            {
              name: 'Rating',
              to: paths.rating,
              active: true
            }
          ]
        },
        {
          name: 'Floating labels',
          to: paths.floatingLabels,
          active: true
        },
        {
          name: 'Wizard',
          to: paths.wizard,
          active: true
        },
        {
          name: 'Validation',
          to: paths.validation,
          active: true
        }
      ]
    },
    {
      name: 'Tables',
      icon: 'table',
      active: true,
      children: [
        {
          name: 'Basic tables',
          to: paths.basicTables,
          active: true
        },
        {
          name: 'Advance tables',
          to: paths.advanceTables,
          active: true
        }
      ]
    },
    {
      name: 'Charts',
      icon: 'chart-line',
      active: true,
      children: [
        {
          name: 'Chartjs',
          to: paths.chartjs,
          active: true
        },
        {
          name: 'D3js',
          to: paths.d3js,
          active: true,
          badge: {
            type: 'success',
            text: 'New'
          }
        },
        {
          name: 'ECharts',
          active: true,
          children: [
            {
              name: 'How to use',
              to: paths.echartsHowToUse,
              active: true
            },
            {
              name: 'Line charts',
              to: paths.lineCharts,
              active: true
            },
            {
              name: 'Bar charts',
              to: paths.barCharts,
              active: true
            },
            {
              name: 'Candlestick charts',
              to: paths.candlestickCharts,
              active: true
            },
            {
              name: 'Geo map',
              to: paths.geoMap,
              active: true
            },
            {
              name: 'Scatter charts',
              to: paths.scatterCharts,
              active: true
            },
            {
              name: 'Pie charts',
              to: paths.pieCharts,
              active: true
            },
            {
              name: 'Radar charts',
              to: paths.radarCharts,
              active: true
            },
            {
              name: 'Heatmap charts',
              to: paths.heatmapCharts,
              active: true
            }
          ]
        }
      ]
    },
    {
      name: 'Icons',
      active: true,
      icon: 'shapes',
      children: [
        {
          name: 'Font awesome',
          to: paths.fontAwesome,
          active: true
        },
        {
          name: 'React icons',
          to: paths.reactIcons,
          active: true
        }
      ]
    },
    {
      name: 'Maps',
      icon: 'map',
      active: true,
      children: [
        {
          name: 'Google map',
          to: paths.googleMap,
          active: true
        },
        {
          name: 'Leaflet map',
          to: paths.leafletMap,
          active: true
        }
      ]
    },
    {
      name: 'Components',
      active: true,
      icon: 'puzzle-piece',
      children: [
        {
          name: 'Alerts',
          to: paths.alerts,
          active: true
        },
        {
          name: 'Accordion',
          to: paths.accordion,
          active: true
        },
        {
          name: 'Animated icons',
          to: paths.animatedIcons,
          active: true
        },
        {
          name: 'Backgrounds',
          to: paths.background,
          active: true
        },
        {
          name: 'Badges',
          to: paths.badges,
          active: true
        },
        {
          name: 'Breadcrumbs',
          to: paths.breadcrumbs,
          active: true
        },
        {
          name: 'Buttons',
          to: paths.buttons,
          active: true
        },
        {
          name: 'Calendar',
          to: paths.calendar,
          active: true
        },
        {
          name: 'Cards',
          to: paths.cards,
          active: true
        },
        {
          name: 'Carousel',
          active: true,
          children: [
            {
              name: 'Bootstrap',
              to: paths.bootstrapCarousel,
              label: 'bootstrap-carousel',
              active: true
            },
            {
              name: 'Slick',
              to: paths.slickCarousel,
              active: true
            }
          ]
        },
        {
          name: 'Collapse',
          to: paths.collapse,
          active: true
        },
        {
          name: 'Cookie notice',
          to: paths.cookieNotice,
          active: true
        },
        {
          name: 'Countup',
          to: paths.countup,
          active: true
        },
        {
          name: 'Draggable',
          to: paths.draggable,
          active: true
        },
        {
          name: 'Dropdowns',
          to: paths.dropdowns,
          active: true
        },
        {
          name: 'List group',
          to: paths.listGroup,
          active: true
        },
        {
          name: 'Modals',
          to: paths.modals,
          active: true
        },
        {
          name: 'Offcanvas',
          to: paths.offcanvas,
          active: true
        },
        {
          name: 'Navs & Tabs',
          active: true,
          children: [
            {
              name: 'Navs',
              to: paths.navs,
              active: true
            },
            {
              name: 'Navbar',
              to: paths.navbar,
              active: true
            },
            {
              name: 'Vertical navbar',
              to: paths.verticalNavbar,
              active: true
            },
            {
              name: 'Top navbar',
              to: paths.topNavbar,
              active: true
            },
            {
              name: 'Double Top',
              to: paths.doubleTopNavbar,
              active: true
            },
            {
              name: 'Combo navbar',
              to: paths.comboNavbar,
              active: true
            },
            {
              name: 'Tabs',
              to: paths.tabs,
              active: true
            }
          ]
        },
        {
          name: 'Pictures',
          active: true,
          children: [
            {
              name: 'Avatar',
              to: paths.avatar,
              active: true
            },
            {
              name: 'Images',
              to: paths.images,
              active: true
            },
            {
              name: 'Figures',
              to: paths.figures,
              active: true
            },
            {
              name: 'Hoverbox',
              to: paths.hoverbox,
              active: true
            },
            {
              name: 'Lightbox',
              to: paths.lightbox,
              active: true
            }
          ]
        },
        {
          name: 'Progress Bar',
          to: paths.progressBar,
          active: true
        },
        {
          name: 'Pagination',
          to: paths.pagination,
          active: true
        },
        {
          name: 'Placeholder',
          to: paths.placeholder,
          active: true
        },
        {
          name: 'Popovers',
          to: paths.popovers,
          active: true
        },
        {
          name: 'Scrollspy',
          to: paths.scrollspy,
          active: true
        },
        {
          name: 'Search',
          to: paths.search,
          active: true
        },
        {
          name: 'Spinners',
          to: paths.spinners,
          active: true
        },
        {
          name: 'Timeline',
          to: paths.timeline,
          active: true
        },
        {
          name: 'Toasts',
          to: paths.toasts,
          active: true
        },
        {
          name: 'Tooltips',
          to: paths.tooltips,
          active: true
        },
        {
          name: 'Treeview',
          to: paths.treeview,
          active: true
        },
        {
          name: 'Typed text',
          to: paths.typedText,
          active: true
        },
        {
          name: 'Videos',
          active: true,
          children: [
            {
              name: 'Embed',
              to: paths.embedVideo,
              active: true
            },
            {
              name: 'React Player',
              to: paths.reactPlayer,
              active: true
            }
          ]
        }
      ]
    },
    {
      name: 'Utilities',
      active: true,
      icon: 'fire',
      children: [
        {
          name: 'Background',
          to: paths.backgroundColor,
          active: true
        },
        {
          name: 'Borders',
          to: paths.borders,
          active: true
        },
        {
          name: 'Colors',
          to: paths.colors,
          active: true
        },
        {
          name: 'Colored links',
          to: paths.coloredLinks,
          active: true
        },
        {
          name: 'Display',
          to: paths.display,
          active: true
        },
        {
          name: 'Flex',
          to: paths.flex,
          active: true
        },
        {
          name: 'Float',
          to: paths.float,
          active: true
        },
        {
          name: 'Grid',
          to: paths.grid,
          active: true
        },
        {
          name: 'Scroll Bar',
          to: paths.scrollBar,
          active: true
        },
        {
          name: 'Position',
          to: paths.position,
          active: true
        },
        {
          name: 'Spacing',
          to: paths.spacing,
          active: true
        },
        {
          name: 'Sizing',
          to: paths.sizing,
          active: true
        },
        {
          name: 'Stretched link',
          to: paths.stretchedLink,
          active: true
        },
        {
          name: 'Text truncation',
          to: paths.textTruncation,
          active: true
        },
        {
          name: 'Typography',
          to: paths.typography,
          active: true
        },
        {
          name: 'Vertical align',
          to: paths.verticalAlign,
          active: true
        },
        {
          name: 'Visibility',
          to: paths.visibility,
          active: true
        }
      ]
    },
    {
      name: 'Widgets',
      icon: 'poll',
      to: paths.widgets,
      active: true
    },
    {
      name: 'Multi level',
      active: true,
      icon: 'layer-group',
      children: [
        {
          name: 'Level two',
          active: true,
          children: [
            {
              name: 'Item 1',
              active: true,
              to: '#!'
            },
            {
              name: 'Item 2',
              active: true,
              to: '#!'
            }
          ]
        },
        {
          name: 'Level three',
          active: true,
          children: [
            {
              name: 'Item 3',
              active: true,
              to: '#!'
            },
            {
              name: 'Item 4',
              active: true,
              children: [
                {
                  name: 'Item 5',
                  active: true,
                  to: '#!'
                },
                {
                  name: 'Item 6',
                  active: true,
                  to: '#!'
                }
              ]
            }
          ]
        },
        {
          name: 'Level four',
          active: true,
          children: [
            {
              name: 'Item 6',
              active: true,
              to: '#!'
            },
            {
              name: 'Item 7',
              active: true,
              children: [
                {
                  name: 'Item 8',
                  active: true,
                  to: '#!'
                },
                {
                  name: 'Item 9',
                  active: true,
                  children: [
                    {
                      name: 'Item 10',
                      active: true,
                      to: '#!'
                    },
                    {
                      name: 'Item 11',
                      active: true,
                      to: '#!'
                    }
                  ]
                }
              ]
            }
          ]
        }
      ]
    }
  ]
};

export const testRoutes: RouteGroup = {
  label: 'TEST',
  roles: ['developer'], // Solo il CEO può vedere le route di test
  children: [
    {
      name: 'Test autenticazione',
      icon: 'shield-alt',
      to: paths.authTest,
      active: true,
      roles: ['developer']
    },
    {
      name: 'Tester navigazione ruoli',
      icon: 'users',
      to: paths.roleNavigationTester,
      active: true,
      roles: ['developer']
    }
  ]
};

export const documentationRoutes: RouteGroup = {
  label: 'Documentazione',
  roles: ['developer'],
  children: [
    {
      name: 'Guida introduttiva',
      icon: 'rocket',
      to: paths.gettingStarted,
      active: true
    },
    {
      name: 'Personalizzazione',
      active: true,
      icon: 'wrench',
      children: [
        {
          name: 'Configurazione',
          to: paths.configuration,
          active: true
        },
        {
          name: 'Stili',
          to: paths.styling,
          active: true
        },
        {
          name: 'Modalità scura',
          to: paths.darkMode,
          active: true
        },
        {
          name: 'Plugin',
          to: paths.plugin,
          active: true
        }
      ]
    },
    {
      name: 'FAQ',
      icon: 'question-circle',
      to: paths.faq,
      active: true
    },
    {
      name: 'File di design',
      icon: 'palette',
      to: paths.designFile,
      active: true
    },
    {
      name: 'Registro modifiche',
      icon: 'code-branch',
      to: paths.changelog,
      active: true
    },
    {
      name: 'Migrazione',
      icon: 'sign-out-alt',
      to: paths.migration,
      active: true,
      badge: {
        type: 'success',
        text: 'Nuovo'
      }
    }
  ]
};

export const developmentRoutes: RouteGroup = {
  label: 'Sviluppo',
  roles: ['developer'], // Solo il CEO può accedere alle route di sviluppo
  children: [
    {
      name: 'Strumenti sviluppatore',
      active: true,
      icon: 'code',
      roles: ['developer'],
      children: [
        {
          name: 'Debug di sistema',
          to: paths.systemDebug,
          active: true,
          icon: 'bug',
          roles: ['developer']
        },
        {
          name: 'Esploratore API',
          to: paths.apiExplorer,
          active: true,
          icon: 'cog',
          roles: ['developer']
        },
        {
          name: 'Admin database',
          to: paths.databaseAdmin,
          active: true,
          icon: 'cog',
          roles: ['developer']
        },
        {
          name: 'Visualizzatore log',
          to: paths.logViewer,
          active: true,
          icon: 'file-alt',
          roles: ['developer']
        },
        {
          name: 'Flag funzionalità',
          to: paths.featureFlags,
          active: true,
          icon: 'flag',
          roles: ['developer']
        }
      ]
    },
    {
      name: 'Cruscotto',
      active: true,
      icon: 'chart-pie',
      roles: ['developer'], // Cruscotto accessibile a tutti
      children: dashboardRoutes.children[0].children
    },
    {
      name: 'App',
      active: true,
      icon: 'rocket',
      roles: ['developer'], // Funzionalità app accessibili a tutti
      children: appRoutes.children
    },
    {
      name: 'Pagine',
      active: true,
      icon: 'file-alt',
      roles: ['developer'], // Gestione pagine per manager+
      children: pagesRoutes.children
    },
    {
      name: 'Moduli',
      active: true,
      icon: 'puzzle-piece',
      roles: ['developer'], // Moduli per amministratori+
      children: modulesRoutes.children
    },
    {
      name: 'Test',
      active: true,
      icon: 'shield-alt',
      roles: ['developer'], // Route di test solo per sviluppatori
      children: testRoutes.children
    },
    {
      name: 'Documentazione',
      active: true,
      icon: 'book',
      roles: ['developer'], // Documentazione per amministratori+
      children: documentationRoutes.children
    }
  ]
};

// Route Orkestra di Produzione - organizzate per livello di accesso del ruolo utente
export const operatorRoutes: RouteGroup = {
  label: 'Operatori',
  roles: ['operator'], // Tutti gli utenti autenticati ed autorizzati
  children: [
    {
      name: 'Cruscotto',
      active: true,
      icon: 'chart-pie',
      to: '/user/dashboard',
      exact: true,
      roles: ['operator']
    },
    {
      name: 'Profilo',
      icon: 'user',
      to: '/user/profile',
      active: true,
      roles: ['operator']
    },
    {
      name: 'Calendario',
      icon: 'calendar-alt',
      to: '/user/calendar',
      active: true,
      roles: ['operator']
    }
  ]
};

export const managerRoutes: RouteGroup = {
  label: 'Gestione',
  roles: ['manager'], // Manager e superiori
  children: [
    {
      name: 'Gestione attività',
      active: true,
      icon: 'project-diagram',
      roles: ['manager'],
      children: [
        {
          name: 'Tutte le attività',
          to: '/tasks',
          active: true,
          roles: ['manager']
        },
        {
          name: 'Crea attività',
          to: '/tasks/create',
          active: true,
          roles: ['manager']
        },
        {
          name: 'Panoramica team',
          to: '/teams',
          active: true,
          roles: ['manager']
        }
      ]
    },
    {
      name: 'Report',
      active: true,
      icon: 'chart-bar',
      roles: ['manager'],
      children: [
        {
          name: 'Performance del team',
          to: '/reports/team',
          active: true,
          roles: ['manager']
        },
        {
          name: 'Report operativi',
          to: '/reports/operations',
          active: true,
          roles: ['manager']
        }
      ]
    }
  ]
};

export const adminRoutes: RouteGroup = {
  label: 'Amministrazione',
  roles: ['administrator'], // Amministratore e superiori
  children: [
    {
      name: 'Gestione flotta',
      active: true,
      icon: 'truck',
      roles: ['administrator'],
      children: [
        {
          name: 'Mezzi',
          to: '/fleet/vehicles',
          active: true,
          roles: ['administrator']
        },
        {
          name: 'Gru',
          to: '/fleet/cranes',
          active: true,
          roles: ['administrator']
        },
        {
          name: 'Tachigrafi',
          to: '/fleet/tachographs',
          active: true,
          roles: ['administrator']
        }
      ]
    },
    {
      name: 'Scadenze',
      to: paths.deadlineReports,
      active: true,
      icon: 'calendar-check',
      roles: ['manager']
    }
    // {
    //   name: 'Analisi',
    //   to: paths.analytics,
    //   active: true,
    //   icon: 'chart-line',
    //   roles: ['administrator']
    // },
    // {
    //   name: 'Report aziendali',
    //   active: true,
    //   icon: 'file-alt',
    //   roles: ['administrator'],
    //   children: [
    //     {
    //       name: 'Report finanziari',
    //       to: '/reports/financial',
    //       active: true,
    //       roles: ['administrator']
    //     },
    //     {
    //       name: 'Analisi delle performance',
    //       to: '/reports/analytics',
    //       active: true,
    //       roles: ['administrator']
    //     }
    //   ]
    // }
  ]
};

export const superAdminRoutes: RouteGroup = {
  label: 'Amministrazione Sistema',
  roles: ['administrator'],
  children: [
    {
      name: 'Gestione utenti',
      active: true,
      icon: 'users',
      to: '/admin/users',
      roles: ['administrator']
    },
    {
      name: 'Impostazioni',
      active: true,
      icon: 'cog',
      to: '/admin/settings',
      roles: ['administrator']
    }
  ]
};

// Development routes (only for development environment)
// managerRoutes are excluded for now..
const routeGroups: RouteGroup[] = [
  superAdminRoutes,
  adminRoutes,
  operatorRoutes,
  developmentRoutes
];

export default routeGroups;
