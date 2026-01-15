import paths, { rootPaths } from 'routes/paths';

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
  label: 'Dashboard',
  labelDisable: true,
  roles: ['developer'], // All authenticated users can access the dashboard
  children: [
    {
      name: 'Dashboard',
      active: true,
      icon: 'chart-pie',
      children: [
        {
          name: 'Default',
          to: rootPaths.root,
          exact: true,
          active: true
        },
        {
          name: 'Analytics',
          to: paths.analytics,
          active: true,
          roles: ['developer'] // Advanced analytics for administrators
        },
        {
          name: 'CRM',
          to: paths.crm,
          active: true
        },
        {
          name: 'Management',
          to: paths.projectManagement,
          active: true,
          roles: ['developer'] // Project management for managers and above
        },
        {
          name: 'SaaS',
          to: paths.saas,
          active: true
        },
        {
          name: 'Support Desk',
          to: paths.supportDesk,
          active: true
        }
      ]
    }
  ]
};
export const appRoutes: RouteGroup = {
  label: 'Applications',
  roles: ['developer'], // Basic functionality for all authorized users
  children: [
    {
      name: 'Calendar',
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
          name: 'Inbox',
          to: paths.emailInbox,
          active: true
        },
        {
          name: 'Email Detail',
          to: paths.emailDetail,
          active: true
        },
        {
          name: 'Compose',
          to: paths.emailCompose,
          active: true
        }
      ]
    },
    {
      name: 'Events',
      icon: 'calendar-day',
      active: true,
      children: [
        {
          name: 'Create Event',
          to: paths.createEvent,
          active: true
        },
        {
          name: 'Event Detail',
          to: paths.eventDetail,
          active: true
        },
        {
          name: 'Event List',
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
          name: 'Activity Log',
          to: paths.activityLog,
          active: true
        },
        {
          name: 'Notifications',
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
      name: 'Support Desk',
      icon: 'ticket-alt',
      active: true,
      children: [
        {
          name: 'Table View',
          to: paths.ticketsTable,
          active: true
        },
        {
          name: 'Card View',
          to: paths.ticketsCard,
          active: true
        },
        {
          name: 'Contacts',
          to: paths.contacts,
          active: true
        },
        {
          name: 'Contact Details',
          to: paths.contactDetails,
          active: true
        },
        {
          name: 'Ticket Preview',
          to: paths.ticketsPreview,
          active: true
        },
        {
          name: 'Quick Links',
          to: paths.quickLinks,
          active: true
        },
        {
          name: 'Reports',
          to: paths.reports,
          active: true
        }
      ]
    }
  ]
};

export const pagesRoutes: RouteGroup = {
  label: 'Pages',
  roles: ['developer'], // Page management for managers and above
  children: [
    {
      name: 'Starter',
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
      name: 'Authentication',
      icon: 'lock',
      active: true,
      children: [
        {
          name: 'Login',
          to: paths.login,
          active: true
        }
      ]
    },
    {
      name: 'User',
      icon: 'user',
      active: true,
      roles: ['developer'], // All users can access their own profile
      children: [
        {
          name: 'Profile',
          to: paths.userProfile,
          active: true,
          roles: ['operator']
        },
        {
          name: 'Settings',
          to: paths.userSettings,
          active: true,
          roles: ['operator']
        }
      ]
    },
    {
      name: 'Pricing',
      icon: 'tags',
      active: true,
      children: [
        {
          name: 'Default Pricing',
          to: paths.pricingDefault,
          active: true
        },
        {
          name: 'Alternative Pricing',
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
          name: 'Basic FAQ',
          to: paths.faqBasic,
          active: true
        },
        {
          name: 'Alternative FAQ',
          to: paths.faqAlt,
          active: true
        },
        {
          name: 'Accordion FAQ',
          to: paths.faqAccordion,
          active: true
        }
      ]
    },
    {
      name: 'Errors',
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
      name: 'Miscellaneous',
      icon: 'thumbtack',
      active: true,
      children: [
        {
          name: 'Associations',
          to: paths.associations,
          active: true
        },
        {
          name: 'Invite People',
          to: paths.invitePeople,
          active: true
        },
        {
          name: 'Privacy Policy',
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
        text: 'New'
      },
      children: [
        {
          name: 'Vertical Navbar',
          to: paths.verticalNavLayout,
          active: true,
          newtab: true
        },
        {
          name: 'Top Nav',
          to: paths.topNavLayout,
          active: true,
          newtab: true
        },
        {
          name: 'Double Top',
          to: paths.doubleTopNavLayout,
          active: true,
          newtab: true
        },
        {
          name: 'Combo Nav',
          to: paths.comboNavLayout,
          active: true,
          newtab: true
        }
      ]
    }
  ]
};

export const modulesRoutes: RouteGroup = {
  label: 'Modules',
  roles: ['developer'], // Administrator and above for development modules
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
      to: paths.tables
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
  roles: ['developer'], // Only CEO can see test routes
  children: [
    {
      name: 'Authentication Test',
      icon: 'shield-alt',
      to: paths.authTest,
      active: true,
      roles: ['developer']
    },
    {
      name: 'Role Navigation Tester',
      icon: 'users',
      to: paths.roleNavigationTester,
      active: true,
      roles: ['developer']
    }
  ]
};

export const documentationRoutes: RouteGroup = {
  label: 'Documentation',
  roles: ['developer'],
  children: [
    {
      name: 'Getting Started',
      icon: 'rocket',
      to: paths.gettingStarted,
      active: true
    },
    {
      name: 'Customization',
      active: true,
      icon: 'wrench',
      children: [
        {
          name: 'Configuration',
          to: paths.configuration,
          active: true
        },
        {
          name: 'Styling',
          to: paths.styling,
          active: true
        },
        {
          name: 'Dark Mode',
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
      name: 'Design File',
      icon: 'palette',
      to: paths.designFile,
      active: true
    },
    {
      name: 'Changelog',
      icon: 'code-branch',
      to: paths.changelog,
      active: true
    },
    {
      name: 'Migration',
      icon: 'sign-out-alt',
      to: paths.migration,
      active: true,
      badge: {
        type: 'success',
        text: 'New'
      }
    }
  ]
};

// Reference Library Routes - consolidated developer/AI reference materials
export const referenceRoutes: RouteGroup = {
  label: 'Reference Library',
  roles: ['developer'],
  children: [
    {
      name: 'Dashboards',
      active: true,
      icon: 'chart-pie',
      roles: ['developer'],
      children: [
        { name: 'Default', to: paths.refDashboardsDefault, active: true },
        { name: 'Analytics', to: paths.refDashboardsAnalytics, active: true },
        { name: 'CRM', to: paths.refDashboardsCrm, active: true },
        { name: 'SaaS', to: paths.refDashboardsSaas, active: true },
        {
          name: 'Project Management',
          to: paths.refDashboardsProjectManagement,
          active: true
        },
        {
          name: 'Support Desk',
          to: paths.refDashboardsSupportDesk,
          active: true
        }
      ]
    },
    {
      name: 'App Examples',
      active: true,
      icon: 'rocket',
      roles: ['developer'],
      children: [
        { name: 'Calendar', to: paths.refAppCalendar, active: true },
        { name: 'Chat', to: paths.refAppChat, active: true },
        { name: 'Kanban', to: paths.refAppKanban, active: true },
        {
          name: 'Email',
          active: true,
          children: [
            { name: 'Inbox', to: paths.refAppEmailInbox, active: true },
            { name: 'Compose', to: paths.refAppEmailCompose, active: true },
            { name: 'Detail', to: paths.refAppEmailDetail, active: true }
          ]
        },
        {
          name: 'Events',
          active: true,
          children: [
            { name: 'Create', to: paths.refAppEventsCreate, active: true },
            { name: 'List', to: paths.refAppEventsList, active: true },
            { name: 'Detail', to: paths.refAppEventsDetail, active: true }
          ]
        },
        {
          name: 'Social',
          active: true,
          children: [
            { name: 'Feed', to: paths.refAppSocialFeed, active: true },
            {
              name: 'Activity Log',
              to: paths.refAppSocialActivityLog,
              active: true
            },
            {
              name: 'Notifications',
              to: paths.refAppSocialNotifications,
              active: true
            },
            { name: 'Followers', to: paths.refAppSocialFollowers, active: true }
          ]
        },
        {
          name: 'Support Desk',
          active: true,
          children: [
            {
              name: 'Table View',
              to: paths.refAppSupportDeskTableView,
              active: true
            },
            {
              name: 'Card View',
              to: paths.refAppSupportDeskCardView,
              active: true
            },
            {
              name: 'Contacts',
              to: paths.refAppSupportDeskContacts,
              active: true
            },
            {
              name: 'Contact Details',
              to: paths.refAppSupportDeskContactDetails,
              active: true
            },
            {
              name: 'Tickets Preview',
              to: paths.refAppSupportDeskTicketsPreview,
              active: true
            },
            {
              name: 'Quick Links',
              to: paths.refAppSupportDeskQuickLinks,
              active: true
            },
            {
              name: 'Reports',
              to: paths.refAppSupportDeskReports,
              active: true
            }
          ]
        }
      ]
    },
    {
      name: 'Components',
      active: true,
      icon: 'puzzle-piece',
      roles: ['developer'],
      children: [
        { name: 'Alerts', to: paths.refComponentsAlerts, active: true },
        { name: 'Accordion', to: paths.refComponentsAccordion, active: true },
        {
          name: 'Animated Icons',
          to: paths.refComponentsAnimatedIcons,
          active: true
        },
        {
          name: 'Backgrounds',
          to: paths.refComponentsBackgrounds,
          active: true
        },
        { name: 'Badges', to: paths.refComponentsBadges, active: true },
        {
          name: 'Breadcrumbs',
          to: paths.refComponentsBreadcrumbs,
          active: true
        },
        { name: 'Buttons', to: paths.refComponentsButtons, active: true },
        { name: 'Calendar', to: paths.refComponentsCalendar, active: true },
        { name: 'Cards', to: paths.refComponentsCards, active: true },
        {
          name: 'Carousel',
          active: true,
          children: [
            {
              name: 'Bootstrap',
              to: paths.refComponentsCarouselBootstrap,
              active: true
            },
            {
              name: 'Slick',
              to: paths.refComponentsCarouselSlick,
              active: true
            }
          ]
        },
        { name: 'Collapse', to: paths.refComponentsCollapse, active: true },
        {
          name: 'Cookie Notice',
          to: paths.refComponentsCookieNotice,
          active: true
        },
        { name: 'Countup', to: paths.refComponentsCountup, active: true },
        { name: 'Draggable', to: paths.refComponentsDraggable, active: true },
        { name: 'Dropdowns', to: paths.refComponentsDropdowns, active: true },
        { name: 'List Group', to: paths.refComponentsListGroup, active: true },
        { name: 'Modals', to: paths.refComponentsModals, active: true },
        { name: 'Offcanvas', to: paths.refComponentsOffcanvas, active: true },
        {
          name: 'Navs & Tabs',
          active: true,
          children: [
            { name: 'Navs', to: paths.refComponentsNavs, active: true },
            { name: 'Navbar', to: paths.refComponentsNavbar, active: true },
            {
              name: 'Vertical Navbar',
              to: paths.refComponentsVerticalNavbar,
              active: true
            },
            {
              name: 'Top Navbar',
              to: paths.refComponentsTopNavbar,
              active: true
            },
            {
              name: 'Double Top Navbar',
              to: paths.refComponentsDoubleTopNavbar,
              active: true
            },
            {
              name: 'Combo Navbar',
              to: paths.refComponentsComboNavbar,
              active: true
            },
            { name: 'Tabs', to: paths.refComponentsTabs, active: true }
          ]
        },
        {
          name: 'Pictures',
          active: true,
          children: [
            { name: 'Avatar', to: paths.refComponentsAvatar, active: true },
            { name: 'Images', to: paths.refComponentsImages, active: true },
            { name: 'Figures', to: paths.refComponentsFigures, active: true },
            { name: 'Hoverbox', to: paths.refComponentsHoverbox, active: true },
            { name: 'Lightbox', to: paths.refComponentsLightbox, active: true }
          ]
        },
        {
          name: 'Progress Bar',
          to: paths.refComponentsProgressBar,
          active: true
        },
        { name: 'Pagination', to: paths.refComponentsPagination, active: true },
        {
          name: 'Placeholder',
          to: paths.refComponentsPlaceholder,
          active: true
        },
        { name: 'Popovers', to: paths.refComponentsPopovers, active: true },
        { name: 'Scrollspy', to: paths.refComponentsScrollspy, active: true },
        { name: 'Search', to: paths.refComponentsSearch, active: true },
        { name: 'Spinners', to: paths.refComponentsSpinners, active: true },
        { name: 'Timeline', to: paths.refComponentsTimeline, active: true },
        { name: 'Toasts', to: paths.refComponentsToasts, active: true },
        { name: 'Tooltips', to: paths.refComponentsTooltips, active: true },
        { name: 'Treeview', to: paths.refComponentsTreeview, active: true },
        { name: 'Typed Text', to: paths.refComponentsTypedText, active: true },
        {
          name: 'Videos',
          active: true,
          children: [
            { name: 'Embed', to: paths.refComponentsVideoEmbed, active: true },
            {
              name: 'React Player',
              to: paths.refComponentsVideoReactPlayer,
              active: true
            }
          ]
        }
      ]
    },
    {
      name: 'Forms',
      active: true,
      icon: 'edit',
      roles: ['developer'],
      children: [
        {
          name: 'Basic',
          active: true,
          children: [
            {
              name: 'Form Control',
              to: paths.refFormsFormControl,
              active: true
            },
            { name: 'Input Group', to: paths.refFormsInputGroup, active: true },
            { name: 'Select', to: paths.refFormsSelect, active: true },
            { name: 'Checks', to: paths.refFormsChecks, active: true },
            { name: 'Range', to: paths.refFormsRange, active: true },
            { name: 'Layout', to: paths.refFormsLayout, active: true }
          ]
        },
        {
          name: 'Advance',
          active: true,
          children: [
            {
              name: 'Advance Select',
              to: paths.refFormsAdvanceSelect,
              active: true
            },
            { name: 'Date Picker', to: paths.refFormsDatePicker, active: true },
            { name: 'Editor', to: paths.refFormsEditor, active: true },
            {
              name: 'Emoji Button',
              to: paths.refFormsEmojiButton,
              active: true
            },
            {
              name: 'File Uploader',
              to: paths.refFormsFileUploader,
              active: true
            },
            { name: 'Input Mask', to: paths.refFormsInputMask, active: true },
            {
              name: 'Range Slider',
              to: paths.refFormsRangeSlider,
              active: true
            },
            { name: 'Rating', to: paths.refFormsRating, active: true }
          ]
        },
        {
          name: 'Floating Labels',
          to: paths.refFormsFloatingLabels,
          active: true
        },
        { name: 'Wizard', to: paths.refFormsWizard, active: true },
        { name: 'Validation', to: paths.refFormsValidation, active: true }
      ]
    },
    {
      name: 'Tables',
      active: true,
      icon: 'table',
      roles: ['developer'],
      to: paths.refTables
    },
    {
      name: 'Icons',
      active: true,
      icon: 'icons',
      roles: ['developer'],
      children: [
        { name: 'Font Awesome', to: paths.refIconsFontAwesome, active: true },
        { name: 'React Icons', to: paths.refIconsReactIcons, active: true }
      ]
    },
    {
      name: 'Maps',
      active: true,
      icon: 'map',
      roles: ['developer'],
      children: [
        { name: 'Google', to: paths.refMapsGoogle, active: true },
        { name: 'Leaflet', to: paths.refMapsLeaflet, active: true }
      ]
    },
    {
      name: 'Widgets',
      to: paths.refWidgets,
      active: true,
      icon: 'th',
      roles: ['developer']
    },
    {
      name: 'Charts',
      active: true,
      icon: 'chart-line',
      roles: ['developer'],
      children: [
        { name: 'Chart.js', to: paths.refChartsChartjs, active: true },
        { name: 'D3.js', to: paths.refChartsD3js, active: true },
        {
          name: 'ECharts',
          active: true,
          children: [
            {
              name: 'How to Use',
              to: paths.refChartsEchartsHowToUse,
              active: true
            },
            {
              name: 'Line Charts',
              to: paths.refChartsEchartsLine,
              active: true
            },
            { name: 'Bar Charts', to: paths.refChartsEchartsBar, active: true },
            {
              name: 'Candlestick',
              to: paths.refChartsEchartsCandlestick,
              active: true
            },
            { name: 'Geo Map', to: paths.refChartsEchartsGeoMap, active: true },
            {
              name: 'Scatter Charts',
              to: paths.refChartsEchartsScatter,
              active: true
            },
            { name: 'Pie Charts', to: paths.refChartsEchartsPie, active: true },
            {
              name: 'Radar Charts',
              to: paths.refChartsEchartsRadar,
              active: true
            },
            { name: 'Heatmap', to: paths.refChartsEchartsHeatmap, active: true }
          ]
        }
      ]
    },
    {
      name: 'Utilities',
      active: true,
      icon: 'fire',
      roles: ['developer'],
      children: [
        { name: 'Background', to: paths.refUtilitiesBackground, active: true },
        { name: 'Borders', to: paths.refUtilitiesBorders, active: true },
        { name: 'Colors', to: paths.refUtilitiesColors, active: true },
        {
          name: 'Colored Links',
          to: paths.refUtilitiesColoredLinks,
          active: true
        },
        { name: 'Display', to: paths.refUtilitiesDisplay, active: true },
        { name: 'Visibility', to: paths.refUtilitiesVisibility, active: true },
        {
          name: 'Stretched Link',
          to: paths.refUtilitiesStretchedLink,
          active: true
        },
        { name: 'Float', to: paths.refUtilitiesFloat, active: true },
        { name: 'Position', to: paths.refUtilitiesPosition, active: true },
        { name: 'Spacing', to: paths.refUtilitiesSpacing, active: true },
        { name: 'Sizing', to: paths.refUtilitiesSizing, active: true },
        {
          name: 'Text Truncation',
          to: paths.refUtilitiesTextTruncation,
          active: true
        },
        { name: 'Typography', to: paths.refUtilitiesTypography, active: true },
        {
          name: 'Vertical Align',
          to: paths.refUtilitiesVerticalAlign,
          active: true
        },
        { name: 'Flex', to: paths.refUtilitiesFlex, active: true },
        { name: 'Grid', to: paths.refUtilitiesGrid, active: true },
        { name: 'Scroll Bar', to: paths.refUtilitiesScrollBar, active: true }
      ]
    },
    {
      name: 'Pages',
      active: true,
      icon: 'file-alt',
      roles: ['developer'],
      children: [
        { name: 'Landing', to: paths.refPagesLanding, active: true },
        { name: 'Starter', to: paths.refPagesStarter, active: true },
        {
          name: 'Pricing',
          active: true,
          children: [
            { name: 'Default', to: paths.refPagesPricingDefault, active: true },
            { name: 'Alt', to: paths.refPagesPricingAlt, active: true }
          ]
        },
        {
          name: 'FAQ',
          active: true,
          children: [
            { name: 'Basic', to: paths.refPagesFaqBasic, active: true },
            { name: 'Alt', to: paths.refPagesFaqAlt, active: true },
            { name: 'Accordion', to: paths.refPagesFaqAccordion, active: true }
          ]
        },
        {
          name: 'Miscellaneous',
          active: true,
          children: [
            {
              name: 'Associations',
              to: paths.refPagesMiscAssociations,
              active: true
            },
            {
              name: 'Invite People',
              to: paths.refPagesMiscInvitePeople,
              active: true
            },
            {
              name: 'Privacy Policy',
              to: paths.refPagesMiscPrivacyPolicy,
              active: true
            }
          ]
        },
        {
          name: 'Layouts',
          active: true,
          children: [
            {
              name: 'Vertical Nav',
              to: paths.refPagesLayoutVerticalNav,
              active: true
            },
            { name: 'Top Nav', to: paths.refPagesLayoutTopNav, active: true },
            {
              name: 'Double Top',
              to: paths.refPagesLayoutDoubleTop,
              active: true
            },
            {
              name: 'Combo Nav',
              to: paths.refPagesLayoutComboNav,
              active: true
            }
          ]
        }
      ]
    },
    {
      name: 'Documentation',
      active: true,
      icon: 'book',
      roles: ['developer'],
      children: [
        {
          name: 'Getting Started',
          to: paths.refDocGettingStarted,
          active: true
        },
        { name: 'Configuration', to: paths.refDocConfiguration, active: true },
        { name: 'Styling', to: paths.refDocStyling, active: true },
        { name: 'Dark Mode', to: paths.refDocDarkMode, active: true },
        { name: 'Plugins', to: paths.refDocPlugins, active: true },
        { name: 'FAQ', to: paths.refDocFaq, active: true },
        { name: 'Design File', to: paths.refDocDesignFile, active: true },
        { name: 'Changelog', to: paths.refDocChangelog, active: true },
        { name: 'Migration', to: paths.refDocMigration, active: true }
      ]
    },
    {
      name: 'Test',
      active: true,
      icon: 'flask',
      roles: ['developer'],
      children: [
        { name: 'Auth Test', to: paths.refTestAuth, active: true },
        {
          name: 'Role Navigation',
          to: paths.refTestRoleNavigation,
          active: true
        }
      ]
    }
  ]
};

// Developer Tools Routes - system utilities for development
export const developmentRoutes: RouteGroup = {
  label: 'Development',
  roles: ['developer'],
  children: [
    {
      name: 'Developer Tools',
      active: true,
      icon: 'code',
      roles: ['developer'],
      children: [
        {
          name: 'System Debug',
          to: paths.systemDebug,
          active: true,
          icon: 'bug',
          roles: ['developer']
        },
        {
          name: 'API Explorer',
          to: paths.apiExplorer,
          active: true,
          icon: 'cog',
          roles: ['developer']
        },
        {
          name: 'Database Admin',
          to: paths.databaseAdmin,
          active: true,
          icon: 'cog',
          roles: ['developer']
        },
        {
          name: 'Log Viewer',
          to: paths.logViewer,
          active: true,
          icon: 'file-alt',
          roles: ['developer']
        },
        {
          name: 'Feature Flags',
          to: paths.featureFlags,
          active: true,
          icon: 'flag',
          roles: ['developer']
        }
      ]
    }
  ]
};

// Orkestra Production Routes - organized by user role access level
export const operatorRoutes: RouteGroup = {
  label: 'Operators',
  roles: ['operator'], // All authenticated and authorized users
  children: [
    {
      name: 'Dashboard',
      active: true,
      icon: 'chart-pie',
      to: '/user/dashboard',
      exact: true,
      roles: ['operator']
    },
    {
      name: 'Profile',
      icon: 'user',
      to: '/user/profile',
      active: true,
      roles: ['operator']
    },
    {
      name: 'Calendar',
      icon: 'calendar-alt',
      to: '/user/calendar',
      active: true,
      roles: ['operator']
    }
  ]
};

export const managerRoutes: RouteGroup = {
  label: 'Management',
  roles: ['manager'], // Manager and above
  children: [
    {
      name: 'Task Management',
      active: true,
      icon: 'project-diagram',
      roles: ['manager'],
      children: [
        {
          name: 'All Tasks',
          to: '/tasks',
          active: true,
          roles: ['manager']
        },
        {
          name: 'Create Task',
          to: '/tasks/create',
          active: true,
          roles: ['manager']
        },
        {
          name: 'Team Overview',
          to: '/teams',
          active: true,
          roles: ['manager']
        }
      ]
    },
    {
      name: 'Reports',
      active: true,
      icon: 'chart-bar',
      roles: ['manager'],
      children: [
        {
          name: 'Team Performance',
          to: '/reports/team',
          active: true,
          roles: ['manager']
        },
        {
          name: 'Operational Reports',
          to: '/reports/operations',
          active: true,
          roles: ['manager']
        }
      ]
    }
  ]
};

export const adminRoutes: RouteGroup = {
  label: 'Administration',
  roles: ['administrator'], // Administrator and above
  children: [
    {
      name: 'Deadlines',
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
  label: 'System Administration',
  roles: ['administrator'],
  children: [
    {
      name: 'User Management',
      active: true,
      icon: 'users',
      to: '/admin/users',
      roles: ['administrator']
    },
    {
      name: 'Settings',
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
  referenceRoutes,
  developmentRoutes
];

export default routeGroups;
