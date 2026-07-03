import paths, { rootPaths } from 'routes/paths';
import type { NavItem as ApiNavItem, NavRealm } from 'store/api/navigationApi';

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
          to: paths.refDashboardsAnalytics,
          active: true,
          roles: ['developer'] // Advanced analytics for administrators
        },
        {
          name: 'CRM',
          to: paths.refDashboardsCrm,
          active: true
        },
        {
          name: 'Management',
          to: paths.refDashboardsProjectManagement,
          active: true,
          roles: ['developer'] // Project management for managers and above
        },
        {
          name: 'SaaS',
          to: paths.refDashboardsSaas,
          active: true
        },
        {
          name: 'Support Desk',
          to: paths.refDashboardsSupportDesk,
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
      to: paths.refAppCalendar,
      active: true
    },
    {
      name: 'Chat',
      icon: 'comments',
      to: paths.refAppChat,
      active: true
    },
    {
      name: 'Email',
      icon: 'envelope-open',
      active: true,
      children: [
        {
          name: 'Inbox',
          to: paths.refAppEmailInbox,
          active: true
        },
        {
          name: 'Email Detail',
          to: paths.refAppEmailDetail,
          active: true
        },
        {
          name: 'Compose',
          to: paths.refAppEmailCompose,
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
          to: paths.refAppEventsCreate,
          active: true
        },
        {
          name: 'Event Detail',
          to: paths.refAppEventsDetail,
          active: true
        },
        {
          name: 'Event List',
          to: paths.refAppEventsList,
          active: true
        }
      ]
    },
    {
      name: 'Kanban',
      icon: ['fab', 'trello'],
      to: paths.refAppKanban,
      active: true
    },
    {
      name: 'Social',
      icon: 'share-alt',
      active: true,
      children: [
        {
          name: 'Feed',
          to: paths.refAppSocialFeed,
          active: true
        },
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
        {
          name: 'Followers',
          to: paths.refAppSocialFollowers,
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
          name: 'Ticket Preview',
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
};

export const pagesRoutes: RouteGroup = {
  label: 'Pages',
  roles: ['developer'], // Page management for managers and above
  children: [
    {
      name: 'Starter',
      icon: 'flag',
      to: paths.refPagesStarter,
      active: true
    },
    {
      name: 'Landing',
      icon: 'globe',
      to: paths.refPagesLanding,
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
          to: paths.refPagesPricingDefault,
          active: true
        },
        {
          name: 'Alternative Pricing',
          to: paths.refPagesPricingAlt,
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
          to: paths.refPagesFaqBasic,
          active: true
        },
        {
          name: 'Alternative FAQ',
          to: paths.refPagesFaqAlt,
          active: true
        },
        {
          name: 'Accordion FAQ',
          to: paths.refPagesFaqAccordion,
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
          to: paths.refPagesLayoutVerticalNav,
          active: true,
          newtab: true
        },
        {
          name: 'Top Nav',
          to: paths.refPagesLayoutTopNav,
          active: true,
          newtab: true
        },
        {
          name: 'Double Top',
          to: paths.refPagesLayoutDoubleTop,
          active: true,
          newtab: true
        },
        {
          name: 'Combo Nav',
          to: paths.refPagesLayoutComboNav,
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
              to: paths.refFormsFormControl,
              active: true
            },
            {
              name: 'Input group',
              to: paths.refFormsInputGroup,
              active: true
            },
            {
              name: 'Select',
              to: paths.refFormsSelect,
              active: true
            },
            {
              name: 'Checks',
              to: paths.refFormsChecks,
              active: true
            },
            {
              name: 'Range',
              to: paths.refFormsRange,
              active: true
            },
            {
              name: 'Layout',
              to: paths.refFormsLayout,
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
              to: paths.refFormsAdvanceSelect,
              active: true
            },
            {
              name: 'Date picker',
              to: paths.refFormsDatePicker,
              active: true
            },
            {
              name: 'Editor',
              to: paths.refFormsEditor,
              active: true
            },
            {
              name: 'Emoji button',
              to: paths.refFormsEmojiButton,
              active: true
            },
            {
              name: 'File uploader',
              to: paths.refFormsFileUploader,
              active: true
            },
            {
              name: 'Input mask',
              to: paths.refFormsInputMask,
              active: true
            },
            {
              name: 'Range slider',
              to: paths.refFormsRangeSlider,
              active: true
            },
            {
              name: 'Rating',
              to: paths.refFormsRating,
              active: true
            }
          ]
        },
        {
          name: 'Floating labels',
          to: paths.refFormsFloatingLabels,
          active: true
        },
        {
          name: 'Wizard',
          to: paths.refFormsWizard,
          active: true
        },
        {
          name: 'Validation',
          to: paths.refFormsValidation,
          active: true
        }
      ]
    },
    {
      name: 'Tables',
      icon: 'table',
      active: true,
      to: paths.refTables
    },
    {
      name: 'Charts',
      icon: 'chart-line',
      active: true,
      children: [
        {
          name: 'ECharts',
          active: true,
          children: [
            {
              name: 'How to use',
              to: paths.refChartsEchartsHowToUse,
              active: true
            },
            {
              name: 'Line charts',
              to: paths.refChartsEchartsLine,
              active: true
            },
            {
              name: 'Bar charts',
              to: paths.refChartsEchartsBar,
              active: true
            },
            {
              name: 'Candlestick charts',
              to: paths.refChartsEchartsCandlestick,
              active: true
            },
            {
              name: 'Geo map',
              to: paths.refChartsEchartsGeoMap,
              active: true
            },
            {
              name: 'Scatter charts',
              to: paths.refChartsEchartsScatter,
              active: true
            },
            {
              name: 'Pie charts',
              to: paths.refChartsEchartsPie,
              active: true
            },
            {
              name: 'Radar charts',
              to: paths.refChartsEchartsRadar,
              active: true
            },
            {
              name: 'Heatmap charts',
              to: paths.refChartsEchartsHeatmap,
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
          to: paths.refIconsFontAwesome,
          active: true
        },
        {
          name: 'React icons',
          to: paths.refIconsReactIcons,
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
          to: paths.refMapsGoogle,
          active: true
        },
        {
          name: 'Leaflet map',
          to: paths.refMapsLeaflet,
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
          to: paths.refComponentsAlerts,
          active: true
        },
        {
          name: 'Accordion',
          to: paths.refComponentsAccordion,
          active: true
        },
        {
          name: 'Animated icons',
          to: paths.refComponentsAnimatedIcons,
          active: true
        },
        {
          name: 'Backgrounds',
          to: paths.refUtilitiesBackground,
          active: true
        },
        {
          name: 'Badges',
          to: paths.refComponentsBadges,
          active: true
        },
        {
          name: 'Breadcrumbs',
          to: paths.refComponentsBreadcrumbs,
          active: true
        },
        {
          name: 'Buttons',
          to: paths.refComponentsButtons,
          active: true
        },
        {
          name: 'Calendar',
          to: paths.refAppCalendar,
          active: true
        },
        {
          name: 'Cards',
          to: paths.refComponentsCards,
          active: true
        },
        {
          name: 'Stat Cards',
          to: paths.refComponentsStatCards,
          active: true
        },
        {
          name: 'Carousel',
          active: true,
          children: [
            {
              name: 'Bootstrap',
              to: paths.refComponentsCarouselBootstrap,
              label: 'bootstrap-carousel',
              active: true
            },
            {
              name: 'Slick',
              to: paths.refComponentsCarouselSlick,
              active: true
            }
          ]
        },
        {
          name: 'Collapse',
          to: paths.refComponentsCollapse,
          active: true
        },
        {
          name: 'Cookie notice',
          to: paths.refComponentsCookieNotice,
          active: true
        },
        {
          name: 'Countup',
          to: paths.refComponentsCountup,
          active: true
        },
        {
          name: 'Draggable',
          to: paths.refComponentsDraggable,
          active: true
        },
        {
          name: 'Dropdowns',
          to: paths.refComponentsDropdowns,
          active: true
        },
        {
          name: 'List group',
          to: paths.refComponentsListGroup,
          active: true
        },
        {
          name: 'Modals',
          to: paths.refComponentsModals,
          active: true
        },
        {
          name: 'Offcanvas',
          to: paths.refComponentsOffcanvas,
          active: true
        },
        {
          name: 'Navs & Tabs',
          active: true,
          children: [
            {
              name: 'Navs',
              to: paths.refComponentsNavs,
              active: true
            },
            {
              name: 'Navbar',
              to: paths.refComponentsNavbar,
              active: true
            },
            {
              name: 'Vertical navbar',
              to: paths.refComponentsVerticalNavbar,
              active: true
            },
            {
              name: 'Top navbar',
              to: paths.refComponentsTopNavbar,
              active: true
            },
            {
              name: 'Double Top',
              to: paths.refComponentsDoubleTopNavbar,
              active: true
            },
            {
              name: 'Combo navbar',
              to: paths.refComponentsComboNavbar,
              active: true
            },
            {
              name: 'Tabs',
              to: paths.refComponentsTabs,
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
              to: paths.refComponentsAvatar,
              active: true
            },
            {
              name: 'Images',
              to: paths.refComponentsImages,
              active: true
            },
            {
              name: 'Figures',
              to: paths.refComponentsFigures,
              active: true
            },
            {
              name: 'Hoverbox',
              to: paths.refComponentsHoverbox,
              active: true
            },
            {
              name: 'Lightbox',
              to: paths.refComponentsLightbox,
              active: true
            }
          ]
        },
        {
          name: 'Progress Bar',
          to: paths.refComponentsProgressBar,
          active: true
        },
        {
          name: 'Pagination',
          to: paths.refComponentsPagination,
          active: true
        },
        {
          name: 'Placeholder',
          to: paths.refComponentsPlaceholder,
          active: true
        },
        {
          name: 'Popovers',
          to: paths.refComponentsPopovers,
          active: true
        },
        {
          name: 'Scrollspy',
          to: paths.refComponentsScrollspy,
          active: true
        },
        {
          name: 'Search',
          to: paths.refComponentsSearch,
          active: true
        },
        {
          name: 'Spinners',
          to: paths.refComponentsSpinners,
          active: true
        },
        {
          name: 'Timeline',
          to: paths.refComponentsTimeline,
          active: true
        },
        {
          name: 'Toasts',
          to: paths.refComponentsToasts,
          active: true
        },
        {
          name: 'Tooltips',
          to: paths.refComponentsTooltips,
          active: true
        },
        {
          name: 'Treeview',
          to: paths.refComponentsTreeview,
          active: true
        },
        {
          name: 'Typed text',
          to: paths.refComponentsTypedText,
          active: true
        },
        {
          name: 'Videos',
          active: true,
          children: [
            {
              name: 'Embed',
              to: paths.refComponentsVideoEmbed,
              active: true
            },
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
      name: 'Utilities',
      active: true,
      icon: 'fire',
      children: [
        {
          name: 'Background',
          to: paths.refUtilitiesBackground,
          active: true
        },
        {
          name: 'Borders',
          to: paths.refUtilitiesBorders,
          active: true
        },
        {
          name: 'Colors',
          to: paths.refUtilitiesColors,
          active: true
        },
        {
          name: 'Colored links',
          to: paths.refUtilitiesColoredLinks,
          active: true
        },
        {
          name: 'Display',
          to: paths.refUtilitiesDisplay,
          active: true
        },
        {
          name: 'Flex',
          to: paths.refUtilitiesFlex,
          active: true
        },
        {
          name: 'Float',
          to: paths.refUtilitiesFloat,
          active: true
        },
        {
          name: 'Grid',
          to: paths.refUtilitiesGrid,
          active: true
        },
        {
          name: 'Scroll Bar',
          to: paths.refUtilitiesScrollBar,
          active: true
        },
        {
          name: 'Position',
          to: paths.refUtilitiesPosition,
          active: true
        },
        {
          name: 'Spacing',
          to: paths.refUtilitiesSpacing,
          active: true
        },
        {
          name: 'Sizing',
          to: paths.refUtilitiesSizing,
          active: true
        },
        {
          name: 'Stretched link',
          to: paths.refUtilitiesStretchedLink,
          active: true
        },
        {
          name: 'Text truncation',
          to: paths.refUtilitiesTextTruncation,
          active: true
        },
        {
          name: 'Typography',
          to: paths.refUtilitiesTypography,
          active: true
        },
        {
          name: 'Vertical align',
          to: paths.refUtilitiesVerticalAlign,
          active: true
        },
        {
          name: 'Visibility',
          to: paths.refUtilitiesVisibility,
          active: true
        }
      ]
    },
    {
      name: 'Widgets',
      icon: 'poll',
      to: paths.refWidgets,
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

export const documentationRoutes: RouteGroup = {
  label: 'Documentation',
  roles: ['developer'],
  children: [
    {
      name: 'Getting Started',
      icon: 'rocket',
      to: paths.refDocGettingStarted,
      active: true
    },
    {
      name: 'Customization',
      active: true,
      icon: 'wrench',
      children: [
        {
          name: 'Configuration',
          to: paths.refDocConfiguration,
          active: true
        },
        {
          name: 'Styling',
          to: paths.refDocStyling,
          active: true
        },
        {
          name: 'Dark Mode',
          to: paths.refDocDarkMode,
          active: true
        },
        {
          name: 'Plugin',
          to: paths.refDocPlugins,
          active: true
        }
      ]
    },
    {
      name: 'FAQ',
      icon: 'question-circle',
      to: paths.refDocFaq,
      active: true
    },
    {
      name: 'Design File',
      icon: 'palette',
      to: paths.refDocDesignFile,
      active: true
    },
    {
      name: 'Changelog',
      icon: 'code-branch',
      to: paths.refDocChangelog,
      active: true
    },
    {
      name: 'Migration',
      icon: 'sign-out-alt',
      to: paths.refDocMigration,
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
          icon: 'envelope',
          children: [
            { name: 'Inbox', to: paths.refAppEmailInbox, active: true },
            { name: 'Compose', to: paths.refAppEmailCompose, active: true },
            { name: 'Detail', to: paths.refAppEmailDetail, active: true }
          ]
        },
        {
          name: 'Events',
          active: true,
          icon: 'calendar-alt',
          children: [
            { name: 'Create', to: paths.refAppEventsCreate, active: true },
            { name: 'List', to: paths.refAppEventsList, active: true },
            { name: 'Detail', to: paths.refAppEventsDetail, active: true }
          ]
        },
        {
          name: 'Social',
          active: true,
          icon: 'share-alt',
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
          icon: 'headphones',
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
        { name: 'Stat Cards', to: paths.refComponentsStatCards, active: true },
        {
          name: 'Carousel',
          active: true,
          icon: 'play-circle',
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
          icon: 'sitemap',
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
          icon: 'image',
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
          icon: 'video',
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
          icon: 'pencil-alt',
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
          icon: 'sliders-h',
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
        {
          name: 'ECharts',
          active: true,
          icon: 'chart-bar',
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
          icon: 'dollar-sign',
          children: [
            { name: 'Default', to: paths.refPagesPricingDefault, active: true },
            { name: 'Alt', to: paths.refPagesPricingAlt, active: true }
          ]
        },
        {
          name: 'FAQ',
          active: true,
          icon: 'question-circle',
          children: [
            { name: 'Basic', to: paths.refPagesFaqBasic, active: true },
            { name: 'Alt', to: paths.refPagesFaqAlt, active: true },
            { name: 'Accordion', to: paths.refPagesFaqAccordion, active: true }
          ]
        },
        {
          name: 'Miscellaneous',
          active: true,
          icon: 'ellipsis-h',
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
          icon: 'columns',
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

// Developer realm — surfaced in the operator sidebar only in dev builds
// (or when VITE_ENABLE_REFERENCE is set). Wraps the existing referenceRoutes
// tree as a v2 NavRealm so NavbarVertical can render it through the same
// realm → section → items path as backend-driven realms.
//
// This is the single hardcoded-nav exception in the frontend. Production
// nav must come from the backend module's NavItems() — do not extend this
// pattern to anything other than the dev-only Orkestra template pages.
export const developerRealm: NavRealm = {
  key: 'developer',
  label: 'Developer',
  sections: [
    {
      // Matches the realm label so NavbarVertical hides the section sub-label
      // (see the `section.label !== realm.label` guard in NavbarVertical.tsx).
      label: 'Developer',
      children: referenceRoutes.children as ApiNavItem[]
    }
  ]
};
