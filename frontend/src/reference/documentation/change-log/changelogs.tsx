export default [
  {
    version: '5.0.0',
    title: `Eleonora's Falcon`,
    publish: '27 July, 2025',
    warning: {
      title: 'Major update!',
      text: `We have updated Falcon from React 18.* to React 19.* version. Please backup your project before upgrading to the latest version. Follow the <a href='/migration' target="_blank" rel="noreferrer">migration guide</a> for proper setup.`
    },
    logs: {
      New: [
        'Plugin: <code>emoji-picker-react</code>',
        'Plugin: <code>imask</code>',
        'Plugin: <code>react-imask</code>',
        'Plugin: <code>react-leaflet-markercluster</code>',
        'Plugin: <code>yet-another-react-lightbox</code>',
        'Component: <code>src/components/doc-components/MigrationToNineteen</code>',
        'Component: <code>src/components/doc-components/MigrationToVite</code>',
        'src/data/migration.js',
        'Scss: <code>src/assets/scss/theme/plugins/_emoji-picker.scss</code>',
        'Scss: <code>src/assets/scss/theme/plugins/_lightbox.scss</code>',
      ],
      Update: [
        'Plugin: React updated to <code>v19</code>.',
        'Plugin: Upgraded to <code>React Router v7</code>: <a href="https://reactrouter.com/upgrading/v6#upgrade-to-v7" target="_blank" rel="noreferrer">See Migration Guide</a>.',
        'Removed <code>forwardRef</code> and added the types support',
        'Refactored Suspense boundaries to align React Router v7 best practices.',
        'Removed propTypes from all components.',
        'All dependencies have been upgraded to the latest version supporting <code>React 19.</code>',
        'Plugin: Removed <code>@changey/react-leaflet-markercluster</code>.',
        'Plugin: Removed <code>@emoji-mart/react</code>',
        'Plugin: Removed <code>react-18-image-lightbox</code>',
        'Plugin: Removed <code>react-text-mask</code>',
        'Component: <code>src/components/doc-components/EmojiMart</code> to <code>src/components/doc-components/EmojiPicker.jsx</code>',
        'Scss: Removed <code>src/assets/scss/theme/plugins/_emoji-mart.scss</code>.',
      ],
      Fix: ['Minor bugs.']
    }
  },
  {
    version: '4.8.0',
    title: 'Red-footed Falcon',
    publish: '05 May, 2025',
    warning: {
      title: 'Major update!',
      text: `We have updated Falcon from Create-React-App to Vite version. Please backup your project before upgrading to the latest version. Follow the <a href='/migration' target="_blank" rel="noreferrer">migration guide</a> for proper setup.`
    },
    logs: {
      New: [
        'Doc: <code>src/components/documentation/Migration</code>',
        '<code>src/hooks/useGetDndSensor</code>',
        '<code>compile-scss</code> file added to root',
        '<code>vite.config.js</code> file added to root',
        'Plugin: <code>rtlcss</code>'
      ],
      Update: [
        `<strong>Migrate Create react app to Vite</strong>`,
        `<code>.env</code> variable changes`,
        `<code>index.html</code> moved from public to root`,
        `Component: <ul>
          <li><code>src/App.js</code></li>
          <li><code>src/hooks/useToggleStyle</code></li>
        </ul>`,
        `Plugin: <ul>
          <li>Bootstrap <code>5.3.3</code> to <code>5.3.5</code></li>
          <li>lottie-react <code>2.4.0</code> to <code>2.4.1</code></li>
        </ul>`,
        `SCSS: <code>src/assets/scss/theme/_scrollbar</code>`
      ],
      Fix: ['Minor bugs.']
    }
  },
  {
    version: '4.7.0',
    title: 'Tundra Peregrine Falcon',
    publish: '24 December, 2024',
    logs: {
      New: [
        'Plugin: <code>@dnd-kit/core</code>',
        'Plugin: <code>@dnd-kit/sortable</code>',
        'Plugin: <code>@dnd-kit/utilities</code>',
        'Plugins: Hosted Tinymce to the public directory'
      ],
      Update: [
        `Plugins: <ul>
          <li>@emotion/styled <code>11.13.0</code> to <code>11.13.5</code></li>
          <li>@hookform/resolvers <code>3.9.0</code> to <code>3.9.1</code></li>
          <li>@react-google-maps/api <code>2.19.3</code> to <code>2.20.3</code></li>
          <li>@tanstack/react-table <code>8.20.1</code> to <code>8.20.5</code></li>
          <li>@testing-library/jest-dom <code>6.4.8</code> to <code>6.6.3</code></li>
          <li>@testing-library/react <code>16.0.0</code> to <code>16.0.1</code></li>
          <li>chart.js <code>4.4.3</code> to <code>4.4.7</code></li>
          <li>dayjs <code>1.11.12</code> to <code>1.11.13</code></li>
          <li>prism-react-renderer <code>2.3.1</code> to <code>2.4.0</code></li>
          <li>rc-slider <code>11.1.5</code> to <code>11.1.7</code></li>
          <li>react-bootstrap <code>2.10.4</code> to <code>2.10.6</code></li>
          <li>react-datepicker <code>7.3.0</code> to <code>7.5.0</code></li>
          <li>react-dropzone <code>14.2.3</code> to <code>14.3.5</code></li>
          <li>react-hook-form <code>7.52.2</code> to <code>7.53.2</code></li>
          <li>react-icons <code>5.2.1</code> to <code>5.4.0</code></li>
          <li>react-live <code>4.1.7</code> to <code>4.1.8</code></li>
          <li>react-select <code>5.8.0</code> to <code>5.8.3</code></li>
          <li>react-textarea-autosize <code>8.5.3</code> to <code>8.5.5</code></li>
          <li>react-toastify <code>10.0.5</code> to <code>10.0.6</code></li>
          <li>tinymce <code>7.3.0</code> to <code>7.5.1</code></li>
          <li>video.js <code>8.17.2</code> to <code>8.20.0</code></li>
          <li>web-vitals <code>4.2.3</code> to <code>4.2.4</code></li>
          <li>yup <code>1.4.0</code> to <code>1.5.0</code></li>
        </ul>`,
        `Components: <ul>
          <li><code>src/providers/KanbanProvider.js</code></li>
          <li><code>src/components/kanban/KanbanContainer.js</code></li>
          <li><code>src/components/kanban/KanbanColumn.js</code></li>
          <li><code>src/components/kanban/TaskCard.js</code></li>
          <li><code>src/components/doc-components/DraggableExample.js</code></li>
          <li><code>src/components/doc-components/charts-example/chartjs/ChartScatter.js</code></li>
        </ul>`,
        `Removed: <ul>
          <li>Plugin: <code>react-beautiful-dnd</code></li>
          <li>Component: <code>src/components/kanban/StrictModeDroppable.js</code></li>
        </ul>`
      ],
      Fix: ['Minor bugs.']
    }
  },
  {
    version: '4.6.0',
    title: 'Taita falcon',
    publish: '22 August, 2024',
    logs: {
      New: [
        '<span class="fw-bold">Vite starter version</span> (Contact <a href="mailto:support@themewagon.com">support</a> with the purchase ID)',
        'Plugin: <code>react-simple-star-rating</code>',
        `Page: <ul>
          <li><code>layouts/vertical-nav-layout</code></li>
          <li><code>layouts/top-nav-layout</code></li>
          <li><code>layouts/double-top-nav-layout</code></li>
          <li><code>layouts/combo-nav-layout</code></li>
        </ul>`
      ],
      Update: [
        'All dependencies have been upgraded to the latest version',
        'React version has been updated from <code>18.2.0</code> to <code>18.3.1</code> and fixes some deprecation',
        'Removed defaultProps from react component',
        'Echarts component updated from <code>ReactEChartsCore</code> to <code>ReactEchart</code>',
        `Page: <ul>
          <li><code>src/components/doc-components/Rating.js</code></li>
        </ul>`,
        `Removed: <ul>
          <li>Plugin: <code>react-rating</code></li>
        </ul>`,
        '<span class="fw-bold">Falcon Next.js starter with App router</span> (Contact <a href="mailto:support@themewagon.com">support</a> with the purchase ID)'
      ],
      Fix: ['Fix echarts issue while changing layouts', 'Minor bugs.']
    }
  },
  {
    version: '4.5.0',
    title: 'Collared falcon',
    publish: '20 Mar, 2024',
    warning: {
      title: 'Update Alert!',
      text: 'We have updated all route paths and advanced tables.'
    },
    logs: {
      New: [
        'Plugin: <code>@tanstack/react-table</code>',
        `Page: <ul>
          <li><code>src/routes/paths.js</code></li>
          <li><code>src/providers/*</code></li>
          <li><code>src/providers/AdvanceTableProvider.js</code></li>
          <li><code>src/hooks/useAdvanceTable.js</code></li>
          <li><code>src/hooks/useSupportDeskTable.js</code></li>
          <li><code>src/features/support-desk/tickets-layout/AgentSelect.js</code></li>
          <li><code>src/features/support-desk/tickets-layout/CardLayout.js</code></li>
          <li><code>src/features/support-desk/tickets-layout/Priority.js</code></li>
          <li><code>src/components/common/advance-table/IndeterminateCheckbox.js</code></li>
        </ul>`
      ],
      Update: [
        'All dependencies have been upgraded to the latest version',
        'React Router has been updated from <code>Routes</code> component to <code>createBrowserRouter</code>',
        'All route paths are now controlled centrally from <code>src/routes/paths.js</code>',
        'All providers are now in <code>src/providers/*</code>',
        'All Advanced Tables have been updated using <code>@tanstack/react-table</code>',
        'Replaced <code>getColor()</code> with <code>getThemeColor()</code>',
        `Page: <ul>
          <li><code>src/index.js</code></li>
          <li><code>src/App.js</code></li>
          <li><code>src/routes/siteMaps.js</code></li>
          <li><code>src/Main.js</code> has been replaced by <code>src/providers/AppProvider.js</code></li>
        </ul>`,
        `SCSS: 
          <ul>
            <li><code>src/assets/scss/theme/plugins/_react-datepicker.scss</code></li>
            <li><code>src/assets/scss/theme/plugins/_toastify.scss</code></li>
          </ul>
        `,
        `Removed: <ul>
          <li><code>src/Main.js</code></li>
          <li><code>src/context/Context.js</code></li>
          <li><code>src/components/common/advance-table/AdvanceTableWrapper.js</code></li>
          <li><code>src/components/dashboards/support-desk/unsolved-tickets/TicketsLayout.js</code></li>
          <li>Plugin: <code>react-table</code></li>
        </ul>`
      ],
      Fix: [
        '<code>e-commerce/product/product-details</code> & <code>e-learning/course/course-details</code> page redirection issue.',
        'Minor bugs.'
      ]
    }
  },
  {
    version: '4.4.0',
    title: 'Bush hawk falcon',
    publish: '23 Nov, 2023',
    logs: {
      New: [
        'Doc: Predefined Date Range. <a href="/forms/advance/date-picker">see example</a>'
      ],
      Update: [
        `Utility classes: <ul>
          <li>
            Replaced <code>.fs-8</code> with <code>.fs-1</code>
          </li>
          <li>
            Replaced <code>.fs-7</code> with <code>.fs-2</code>
          </li>
          <li>
            Replaced <code>.fs-6</code> with <code>.fs-3</code>
          </li>
          <li>
            Replaced <code>.fs-5</code> with <code>.fs-4</code>
          </li>
          <li>
            Replaced <code>.fs-4</code> with <code>.fs-5</code>
          </li>
          <li>
            Replaced <code>.fs-3</code> with <code>.fs-6</code>
          </li>
          <li>
            Replaced <code>.fs-2</code> with <code>.fs-7</code>
          </li>
          <li>
            Replaced <code>.fs-1</code> with <code>.fs-8</code>
          </li>
          <li>
            Replaced <code>.fs-0</code> with <code>.fs-9</code>
          </li>
          <li>
            Replaced <code>.fs--1</code> with <code>.fs-10</code>
          </li>
          <li>
            Replaced <code>.fs--2</code> with <code>.fs-11</code>
          </li>
        </ul>`
      ],
      Fix: ['Minor bugs.']
    }
  },
  {
    version: '4.3.0',
    title: 'Common kestrel',
    publish: '14 Oct, 2023',
    warning: {
      title: 'Major update!',
      text: 'We have updated Bootstrap version to 5.3.2 and re-organized SCSS.'
    },
    logs: {
      New: [
        'JS : <code> src/components/navbar/top/ThemeControlDropdown.js </code>',
        'SCSS : <code> src/assets/scss/theme/_variables-dark.scss </code>',
        `Utility classes: <ul>
          <li>
            <code>bg-body-emphasis</code>
          </li>
          <li>
            <code>bg-body-quaternary</code>
          </li>
          <li>
            <code>text-{color}-emphasis</code>
          </li>
          <li>
            <code>border-{color}-subtle</code>
          </li>
          <li>
            <code>dark__bg-opacity-{0|25|50|75|85|100}</code>
          </li>
        </ul>`
      ],
      Update: [
        'Bootstrap to <code> 5.3.2 </code>',
        'Theme change <a href="/documentation/dark-mode">see documentation</a>',
        'Styling <a href="/documentation/styling">see documentation</a>',
        'JS : <code> src/config.js </code>',
        'SCSS : <code> src/assets/scss/theme/_variables.scss </code>',
        'SCSS : <code> src/assets/scss/theme/root/light.scss </code>',
        'SCSS : <code> src/assets/scss/theme/root/_dark.scss </code>',
        'Overall SCSS retouched',
        'Using subtle colors in <code>Alert</code>, <code>Badges</code> & <code>List-group</code> components',
        `Utility classes: <ul>
          <li>
            <code>z-index-*</code> is now <code>z-*</code>
          </li>
          <li>
            <code>bg-soft-{color}</code> is now <code>bg-{color}-subtle</code>
          </li>
          <li>
            <code>bg-light</code> is now <code>bg-body-tertiary</code>
          </li>
          <li>
            <code>bg-{light|dark|black|white}, text-{light|dark|black|white}</code> now representing the actual color in both mode
          </li>
        </ul>`
      ],
      Fix: ['Minor bugs.']
    }
  },
  {
    version: '4.2.0',
    title: 'Sooty falcon',
    publish: '08 June, 2023',
    logs: {
      New: [
        'Plugin : <code> yup </code>',
        'Plugin : <code> @hookform/resolvers </code>',
        'Page : <code> e-commerce/product/add-product </code>'
      ],
      Update: [
        'Doc: <code>forms/advance/advance-select</code> validation',
        'Doc: <code>forms/advance/editor</code> validation',
        'Doc: <code>forms/advance/file-uploader</code> validation',
        '<code>@fullcalendar/bootstrap to 6.1.8 </code>',
        '<code>@fullcalendar/daygrid to 6.1.8 </code>',
        '<code>@fullcalendar/interaction to 6.1.8 </code>',
        '<code>@fullcalendar/list to 6.1.8 </code>',
        '<code>@fullcalendar/react to 6.1.8 </code>',
        '<code>@fullcalendar/timegrid to 6.1.8 </code>',
        '<code>prism-react-renderer to 2.0.4</code>',
        '<code>rc-slider to 10.2.0</code>',
        '<code>react-dropzone to 14.2.3</code>',
        '<code>react-live to 4.1.2</code>',
        '<code>simplebar-react to 3.2.4</code>',
        '<code>uuid to 9.0.0</code>',
        '<code>video.js to 8.3.0</code>'
      ],
      Fix: ['<code>react-slick</code> issues', 'Minor bugs.']
    }
  },
  {
    version: '4.1.0',
    title: 'Lanner falcon',
    publish: '29 March, 2023',
    logs: {
      New: [
        'Plugin : <code> react-range </code>',
        'Plugin : <code> react-text-mask </code>',
        'Module: <code>forms/advance/input-mask</code>',
        'Module: <code>forms/advance/range-slider</code>'
      ],
      Update: [
        '<code>react-router</code> to <code>6.8.1 </code>',
        '<code>src/app.js </code>',
        'All routes are now in <code>src/routes/index.js</code>',
        '<code>src/routes/routes.js</code> is currently <code>src/routes/siteMaps.js</code>'
      ],
      Delete: ['<code>src/layouts/Layout</code>'],
      Fix: ['Tooltip flickering issue.', 'Other fixes.']
    }
  },
  {
    version: '4.0.0',
    title: 'Laggar falcon',
    publish: '22 December, 2022',
    warning: {
      title: 'Major update!',
      text: 'We have updated React version to 18 and multiple packages in this version listed below. To learn more about upgrading to react 18',
      link: 'https://reactjs.org/blog/2022/03/08/react-18-upgrade-guide.html'
    },
    logs: {
      New: [
        'Plugin : <code> webpack </code>',
        'Plugin : <code> @emoji-mart/react </code>'
      ],
      Update: [
        '<code>React to 18.2.0 </code>',
        '<code>react-dom to 18.2.0 </code>',
        '<code>react-scripts to 5.0.1 </code>',
        '<code>@fullcalendar/bootstrap to 5.11.3 </code>',
        '<code>@fullcalendar/daygrid to 5.11.3 </code>',
        '<code>@fullcalendar/interaction to 5.11.3 </code>',
        '<code>@fullcalendar/list to 5.11.3 </code>',
        '<code>@fullcalendar/react to 5.11.3 </code>',
        '<code>@fullcalendar/timegrid to 5.11.3 </code>',
        '<code>@react-leaflet/core to 2.1.0 </code>',
        '<code>@tinymce/tinymce-react to 4.2.0 </code>',
        '<code>leaflet to 1.9.3 </code>',
        '<code>react-leaflet to 4.2.0 </code>',
        '<code>react-leaflet-markercluster to @changey/react-leaflet-markercluster</code>',
        '<code>google-map-react to @react-google-maps/api</code>',
        '<code>chart.js to 4.0.1 </code>',
        '<code>emoji-mart to 5.3.3 </code>',
        '<code>react-beautiful-dnd to 13.1.1 </code>',
        '<code>react-image-lightbox to react-18-image-lightbox </code>',
        '<code>react-chartjs-2 to 5.0.1 </code>',
        '<code>react-datepicker to 4.8.0 </code>',
        '<code>react-hook-form to 7.40.0 </code>',
        '<code>react-select to 5.7.0 </code>',
        '<code>react-simple-typewriter to 5.0.1 </code>',
        '<code>react-slick to 0.29.0 </code>',
        '<code>react-table to 7.8.0 </code>',
        '<code>react-textarea-autosize to 8.4.0 </code>'
      ],
      Fix: ['Minor bugs.']
    }
  },
  {
    version: '3.9.0',
    title: 'Black falcon',
    publish: '22 December, 2022',
    logs: {
      New: [
        'Dashboard : <code> dashboard/support-desk</code>',
        'Page : <code> support-desk/table-view</code>',
        'Page : <code> support-desk/card-view</code>',
        'Page : <code> support-desk/contacts</code>',
        'Page : <code> support-desk/contact-details</code>',
        'Page : <code> support-desk/tickets-preview</code>',
        'Page : <code> support-desk/quick-links</code>',
        'Page : <code> support-desk/reports</code>',
        'SCSS : <code> src/assets/scss/theme/_support-desk.scss </code>',
        'SCSS : <code> src/assets/scss/theme/_progress.scss </code>'
      ],
      Update: ['Padding updated p-card to p-x1'],
      Migration: {
        Add: [
          '<code>src/components/dashboards/support-desk/**/*</code>',
          '<code>src/features/support-desk/**/*</code>',
          '<code>src/assets/scss/theme/_support-desk.scss</code>',
          '<code>src/assets/scss/theme/_progress.scss</code>',
          '<code>src/data/dashboard/support-desk.js</code>',
          '<code>src/data/support-desk/contactsData.js',
          '<code>src/data/support-desk/contactDetailsData.js',
          '<code>src/data/support-desk/ticketsPreviewData.js',
          '<code>src/data/support-desk/quickLinksData.js',
          '<code>src/data/support-desk/reportsData.js'
        ]
      }
    }
  },
  {
    version: '3.8.0',
    title: 'Gyrfalcon',
    publish: '29 Novembar, 2022',
    logs: {
      New: [
        'Navbar : <code> Double-top </code>',
        'Dashboard : <code> dashboard/lms </code>',
        'Page : <code> e-learning/course/course-list </code>',
        'Page : <code> e-learning/course/course-grid </code>',
        'Page : <code> e-learning/course/course-details </code>',
        'Page : <code> e-learning/course/create-a-course </code>',
        'Page : <code> e-learning/student-overview </code>',
        'Page : <code> e-learning/trainer-profile </code>',
        'Doc : <code> charts/d3js </code>',
        'SCSS : <code> src/assets/scss/theme/_e-learning.scss </code>',
        'SCSS : <code> src/assets/scss/theme/_bottom-bar.scss </code>',
        'SCSS : <code> src/assets/scss/theme/_focus.scss </code>',
        'SCSS : <code> src/assets/scss/theme/_collapse.scss </code>',
        'SCSS : <code> src/assets/scss/theme/_double-top-nav-layout.scss </code>',
        'Plugin : <code> D3.js </code>'
      ],
      Update: [
        'Bootstrap to <code> 5.2.2 </code>',
        'React Bootstrap to <code> 2.6.0 </code>'
      ],
      Fix: ['Minor bugs.'],
      Migration: {
        Add: [
          '<code>src/components/dashboards/lms/**/*</code>',
          '<code>src/features/e-learning/**/*</code>',

          '<code>src/assets/scss/theme/_e-learning.scss</code>',
          '<code>src/assets/scss/theme/_bottom-bar.scss</code>',
          '<code>src/assets/scss/theme/_focus.scss</code>',
          '<code>src/assets/scss/theme/_collapse.scss</code>',
          '<code> src/assets/scss/theme/_double-top-nav-layout.scss </code>',

          '<code>src/hooks/useCourses.js</code>',
          '<code>src/hooks/useVisibilityObserver.js</code>',

          '<code>src/data/elearning/courseData.js</code>',
          '<code>src/data/elearning/courseDetails.js</code>',
          '<code>src/data/elearning/createCourse.js</code>',
          '<code>src/data/elearning/studentOverview.js</code>'
        ],
        Update: [
          '<code>src/context/Context.js</code>',
          '<code>package.json</code>'
        ]
      }
    }
  },
  {
    version: '3.7.1',
    title: 'Bat falcon',
    publish: '16 Septembor, 2022',
    logs: {
      Fix: ['process env issue'],
      Migration: {}
    }
  },
  {
    version: '3.7.0',
    title: 'Prairie falcon',
    publish: '11 Septembor, 2022',
    logs: {
      New: ['SCSS: to <code>src/scss/theme/_maps.scss</code>'],
      Update: [
        'Bootstrap to <code> 5.2.0 </code>',
        'All SCSS files',
        'CSS variables for gray',
        '<code>bg-{color}</code> classes replaced with <code>text-{color}</code> in <code>hr</code>',
        "JS: <code>getColor('{100/200/300}')</code> changed with <code>getColor('{gray-100/gray-200/gray-300}') </code>",
        'Doc: <a href="/utilities/borders">Border styles updated</a>',
        'Doc: <a href="/documentation/styling">Styling</a>',
        'Plugin: Replaced <code>react-type-animation</code> with <code>react-simple-typewriter</code>'
      ],

      Migration: {
        Update: [
          '<code>src/components/doc-components/Tables.js</code>',
          '<code>src/components/dashboards/crm/LocationBySession/LocationBySession.js</code>',
          '<code>src/components/dashboards/default/SharedFiles.js</code>',
          '<code>src/components/dashboards/default/MarketShare.js</code>',
          '<code>src/components/dashboards/default/RunningProjects.js</code>',
          '<code>src/components/dashboards/default/StorageStatus.js</code>',
          '<code>src/components/navbar/vertical/PurchaseCard.js</code>',
          '<code>src/components/pages/landing/Cta.js</code>',
          '<code>src/features/e-commerce/Invoice.js</code>',
          '<code>src/features/chat/content/MessageTextArea.js</code>',
          '<code>src/components/pages/landing/Hero.js</code>',
          '<code>src/components/dashboards/crm/recent-leads/RecentLeads.js</code>'
        ]
      }
    }
  },
  {
    version: '3.6.0',
    title: 'Aplomado falcon',
    publish: '04 August, 2022',
    logs: {
      Update: [
        'Plugin: Replaced <code>google-maps-react</code> with <code>google-map-react</code>',
        'Plugin: Replaced <code>react-lottie</code> with <code>lottie-react</code>',
        'Plugin: Replaced <code>react-typed</code> with <code>react-type-animation</code>'
      ],
      Fix: ['Npm peer dependencies issues', 'User styles issue'],

      Migration: {
        Update: [
          '<code>src/features/events/event-detail/EventDetailsGmap.js</code>',
          '<code>src/components/doc-components/GoogleMapExample.js</code>',
          '<code>src/components/map/GoogleMap.js</code>',
          '<code>src/hooks/useToggleStyle.js</code>',
          '<code>src/assets/scss/theme.scss</code>',
          '<code>src/components/doc-components/AnimatedIcons.js</code>',
          '<code>src/components/wizard/Success.js</code>',
          '<code>src/components/wizard/WizardModal.js</code>'
        ]
      }
    }
  },
  {
    version: '3.5.1',
    title: 'Barbary falcon',
    publish: '06 july, 2022',
    logs: {
      Update: ['React bootstrap version to <code>v2.4.0</code>'],
      Fix: ['Tinymce border color issue in RTL'],

      Migration: {
        Update: [
          '<code>src/components/authentication/LoginForm.js</code>',
          '<code>src/assets/scss/theme/plugins/_react-select.scss</code>',
          '<code>src/assets/scss/theme/plugins/_tinymce.scss</code>',
          '<code>src/assets/scss/theme/plugins/_react-datepicker.scss</code>'
        ]
      }
    }
  },
  {
    version: '3.5.0',
    title: 'Lesser falcon',
    publish: '12 May, 2022',
    logs: {
      Update: ['React bootstrap version to <code>v2.3.1</code>'],
      Fix: ['Vertical navbar card style in dark mode', 'Dropdown caret issue'],

      Migration: {
        Update: [
          '<code>src/components/pages/landing/LandingRightSideNavItem.js</code>',
          '<code>src/components/navbar/top/ProfileDropdown.js</code>',
          '<code>src/components/navbar/top/NotificationDropdown.js</code>',
          '<code>src/components/navbar/top/NineDotMenu.js</code>',
          '<code>src/components/navbar/top/NavbarDropdown.js</code>',
          '<code>src/components/doc-components/Pagination.js</code>',
          '<code>src/features/kanban/TaskCard.js</code>',
          '<code>src/components/dashboards/project-management/project-statistics/ProjectTable.js</code>',
          '<code>src/assets/scss/theme/root/_dark.scss</code>',
          '<code>src/assets/scss/theme/root/_light.scss</code>',
          '<code>src/assets/scss/theme/_navbar-top.scss</code>',
          '<code>src/assets/scss/theme/_navbar.scss</code>',
          '<code>src/assets/scss/theme/_variables.scss</code>'
        ]
      }
    }
  },
  {
    version: '3.4.0',
    title: 'Peregrine falcon',
    publish: '21 March, 2022',
    logs: {
      New: ['Reset button on setting panel', 'Page: Faq in Documentation'],
      Update: [
        'React bootstrap version to <code>v2.2.1</code>',
        'Plugin: <code>react-hook-form</code>'
      ],
      Fix: ['React select issue', 'Color opacity issue'],

      Migration: {
        New: ['<code>src/components/documentation/Faq.js</code>'],
        Update: [
          
          '<code>src/helpers/initFA.js</code>',
          '<code>src/hooks/useToggleStyle.js</code>',
          '<code>src/reducers/configReducer.js</code>',
          '<code>src/Main.js</code>',
          '<code>src/layouts/Layout.js</code>',
          '<code>src/reducers/configReducer.js</code>',
          '<code>src/routes/routes.js</code>',
          '<code>src/features/events/create-an-event/CreateEvent.js</code>',
          '<code>src/features/events/create-an-event/EventDetails.js</code>',
          '<code>src/features/events/create-an-event/EventOtherInfo.js</code>',
          '<code>src/components/pages/faq/faq-accordion/FaqAccordionItem.js</code>',
          '<code>src/components/dashboards/crm/CrmStats.js</code>',
          '<code>src/components/dashboards/crm/ToDoList.js</code>',
          '<code>src/data/faqs.js</code>',
          '<code>src/config.js</code>',
          '<code>src/assets/scss/theme/root/_dark.scss</code>',
          '<code>src/assets/scss/theme/root/_light.scss</code>',
          '<code>src/assets/scss/theme/root/_override.scss</code>',
          '<code>src/assets/scss/theme/_colors.scss</code>',
          '<code>src/assets/scss/theme/_functions.scss</code>',
          '<code>src/assets/scss/theme/_mixins.scss</code>',
          '<code>src/assets/scss/theme/_utilities.scss</code>',
          '<code>src/assets/scss/theme/_variables.scss</code>',
          '<code>src/assets/scss/theme/plugins/_react-select.scss</code>',

          '<code>package.json</code>'
        ],
        Delete: ['<code>src/assets/scss/theme/mixins/_utilities.scss</code>']
      }
    }
  },
  {
    version: '3.3.0',
    title: 'Bateleur falcon',
    badgeTitle: '!! Breaking Change !!',
    publish: '17 February, 2022',
    logs: {
      Update: [
        "React router dom v5 to v6. </br> <span class='text-danger'> This is a breaking change. </br></span> Please view the official guide  <a target=`_blank` href='https://reactrouter.com/docs/en/v6/upgrading/v5'> migrating react-router v5 to v6 here</a> for more details.",
        'Plugin: <code>React toastify</code>',
        'Plugin: <code>Fullcalendar</code>'
      ],
      Fix: [
        'Toastify issue',
        'Fullcalendar issue',
        'Doc: Broken links',
        'Minor bugs'
      ],

      Migration: {
        Update: [
          '<code>src/features/e-commerce/product/product-details/ProductDetails.js</code>',
          '<code>src/features/email/email-detail/EmailDetailHeader.js</code>',
          '<code>src/components/authentication/modal/ModalAuth.js</code>',
          '<code>src/layouts/Layout.js</code>',
          '<code>src/layouts/MainLayout.js</code>',
          '<code>src/layouts/ErrorLayout.js</code>',
          '<code>src/components/navbar/vertical/NavbarVerticalMenu.js</code>',
          '<code>src/features/kanban/KanbanProvider.js</code>',
          '<code>src/features/social/feed/AddToFeed.js</code>',
          '<code>src/features/social/feed/Comments.js</code>',
          '<code>src/features/social/feed/Feed.js</code>',
          '<code>src/features/social/feed/FeedCardHeader.js</code>',
          '<code>src/components/dashboards/crm/ToDoList.js</code>',
          '<code>src/components/doc-components/Cards.js</code>',
          '<code>src/components/navbar/top/NineDotMenu.js</code>',
          '<code>src/components/navbar/top/ProfileDropdown.js</code>',
          '<code>src/data/autocomplete/autocomplete.js</code>',
          '<code>src/data/people.js</code>',
          '<code>src/widgets/OthersWidgets.js</code>',
          '<code>src/components/dashboards/crm/index.js</code>',
          '<code>src/components/dashboards/e-commerce/index.js</code>',
          '<code>src/components/dashboards/project-management/index.js</code>',
          '<code>src/components/dashboards/saas/index.js</code>',
          '<code>src/features/e-commerce/product/Products.js</code>',
          '<code>src/components/dashboards/project-management/ToDoList.js</code>',
          '<code>src/components/navbar/top/NotificationDropdown.js</code>',
          '<code>src/data/notification/notification.js</code>',
          '<code>src/features/email/inbox/InboxRow.js</code>',
          '<code>src/components/authentication/ForgetPasswordForm.js</code>',
          '<code>src/components/authentication/LockScreenForm.js</code>',
          '<code>src/components/authentication/LoginForm.js</code>',
          '<code>src/components/authentication/PasswordResetForm.js</code>',
          '<code>src/components/authentication/RegistrationForm.js</code>',
          '<code>src/data/calendar/events.js</code>',
          '<code>src/features/kanban/ModalAttachmentContent.js</code>',
          '<code>src/components/doc-components/Checks.js</code>',
          '<code>src/components/doc-components/FormControl.js</code>',
          '<code>src/components/doc-components/FormLayout.js</code>',
          '<code>src/components/doc-components/InputGroup.js</code>',
          '<code>src/components/doc-components/Range.js</code>',
          '<code>src/components/doc-components/Select.js</code>',
          '<code>src/components/doc-components/FloatingLabels.js</code>',
          '<code>src/components/doc-components/FormValidation.js</code>',
          '<code>src/components/doc-components/Scrollspy.js</code>',

          '<code>src/assets/scss/theme/plugins/_full-calendar.scss</code>',
          '<code>src/assets/scss/theme/helpers/_borders.scss</code>',
          '<code>src/assets/scss/theme/_radio-select.scss</code>',

          '<code>package.json</code>'
        ],
        Delete: [
          '<code>src/layouts/MainRoutes.js</code>',
          '<code>src/components/pages/ComingSoon.js</code>',
          '<code>src/components/authentication/card/AuthCardRoutes.js</code>',
          '<code>src/components/authentication/split/AuthSplitRoutes.js</code>',
          '<code>src/components/authentication/simple/AuthSimpleRoutes.js</code>'
        ]
      }
    }
  },
  {
    version: '3.2.0',
    title: 'Oriental hobby',
    publish: '31 January, 2022',
    logs: {
      New: [
        'Timeline component',
        'Add custom field form in create an event page'
      ],
      Fix: ['Other minor bugs'],

      Migration: {
        Add: [
          '<code>src/components/doc-components/Timeline.js</code>',
          '<code>src/assets/scss/theme/_timeline.scss</code>'
        ],

        Update: [
          '<code>src/components/navbar/top/NineDotMenu.js</code>',
          '<code>src/features/events/create-an-event/CreateEvent.js</code>',
          '<code>src/features/events/create-an-event/EventUpload.js</code>',
          '<code>src/features/events/create-an-event/EventOtherInfo.js</code>',
          '<code>src/features/events/create-an-event/EventScheduleItem.js</code>',
          '<code>src/features/events/create-an-event/EventCustomField.js</code>',
          '<code>src/features/events/create-an-event/EventCustomFieldItem.js</code>',
          '<code>src/components/navbar/top/ProfileDropdown.js</code>',
          '<code>src/helpers/initFA.js</code>',
          '<code>src/layouts/MainRoutes.js</code>',
          '<code>src/routes/routes.js</code>',
          '<code>src/helpers/utils.js</code>',
          '<code>src/assets/scss/theme/_navbar.scss</code>',

          '<code>package.json</code>'
        ]
      }
    }
  },
  {
    version: '3.1.0',
    title: 'Nankeen kestrel',
    publish: '27 December, 2021',
    logs: {
      New: ['Component: 9 dot menu on top navbar'],
      Update: ['React-Bootstrap to v2.0.4', 'Other minor bugs'],

      Migration: {
        Add: ['<code>src/components/navbar/topNineDotMenu.js</code>'],
        Update: [
          '<code>src/assets/scss/theme/_navbar.scss</code>',
          '<code>src/assets/scss/theme/_navbar-top.scss</code>',
          '<code>src/components/navbar/top/ProfileDropdown.js</code>',
          '<code>src/components/navbar/top/TopNavRightSideNavItem.js</code>',
          '<code>src/components/navbar/top/NotificationDropdown.js</code>',
          '<code>src/components/navbar/top/CartNotification.js</code>',
          '<code>src/components/navbar/top/NavbarTopDropDownMenus.js</code>',
          '<code>src/components/navbar/top/NavbarNavLink.js</code>',
          '<code>src/components/notification/Notification.js</code>',
          '<code>src/components/navbar/vertical/ToggleButton.js</code>',
          '<code>src/components/navbar/top/ProfileDropdown.js</code>',
          '<code>package.json</code>'
        ]
      }
    }
  },
  {
    version: '3.0.0',
    title: 'Amur',
    publish: '4 December, 2021',
    logs: {
      New: [
        'Page : <code> Kanban </code>',
        'Page : <code> Widgets </code>',
        'Doc : <code> Chartjs </code>',
        'Doc : <code> Line charts </code>',
        'Doc : <code> Bar charts </code>',
        'Doc : <code> Candlestick charts </code>',
        'Doc : <code> Geo map </code>',
        'Doc : <code> Scatter charts </code>',
        'Doc : <code> Pie charts </code>',
        'Doc : <code> Radar charts </code>',
        'Doc : <code> Heatmap charts </code>',
        'Doc : <code> How to use </code>',
        'Doc : <code> Google map </code>',
        'Doc : <code> Leaflet map </code>',
        'Doc : <code> Calendar </code>',
        'Doc : <code> Cookie notice </code>',
        'Doc : <code> Countup </code>',
        'Doc : <code> Draggable </code>',
        'Doc : <code> Emoji button </code>',
        'Doc : <code> React player </code>',
        'Doc : <code> React icons </code>',
        'Doc : <code> Scroll Bar </code>',
        'Doc : <code> Scrollspy </code>',
        'Doc : <code> Treeview </code>',
        'Component : <code> Treeview </code>',
        'Component : <code> CookieNotice </code>',
        'Plugin : <code>react-beautiful-dnd</code>',
        'Plugin : <code>react-icons</code>',
        'Plugin : <code>react-use-scrollspy</code>'
      ],
      Update: [
        'React-Bootstrap to v2.0.3',
        'Plugin : <strong>Removed</strong> <code>react-scrollspy</code>',
        'Plugin : <strong>Removed</strong> <code>video.js</code>',
        'Component : <strong>Removed</strong> <code>FalconVideoJS</code>',
        'Doc : Renamed bootstrap-components directory to docs-components'
      ],

      Migration: {
        Add: [
          '<code>src/features/kanban/**/*</code>',
          '<code>src/components/doc-components/charts-example/**/*</code>',
          '<code>src/widgets/**/*</code>',
          '<code>src/data/kanban.js</code>',
          '<code>src/helpers/chartjs-utils.js</code>',
          '<code>src/reducers/kanbanReducer.js</code>',
          '<code>src/components/common/CookieNotice.js</code>',
          '<code>src/components/common/Treeview.js</code>',
          '<code>src/components/doc-components/TreeviewExample.js</code>',
          '<code>src/components/doc-components/GoogleMapExample.js</code>',
          '<code>src/components/doc-components/LeafletMapExample.js</code>',
          '<code>src/components/doc-components/EmojiMart.js</code>',
          '<code>src/components/doc-components/DraggableExample.js</code>',
          '<code>src/components/doc-components/CalendarExample.js</code>',
          '<code>src/components/doc-components/CookieNoticeExample.js</code>',
          '<code>src/components/doc-components/CountUp.js</code>',
          '<code>src/components/doc-components/ReactIcons.js</code>',
          '<code>src/components/doc-components/ReactPlayerExample.js</code>',
          '<code>src/components/doc-components/Scrollbar.js</code>',
          '<code>src/components/doc-components/Scrollspy.js</code>',
          '<code>src/components/doc-components/DraggableExample.js</code>',
          '<code>src/assets/json/usa.json</code>',
          '<code>src/assets/scss/theme/_treeview.scss</code>'
        ],
        Update: [
          '<code>src/assets/scss/theme/_dropdown.scss</code>',
          '<code>src/assets/scss/theme/_kanban.scss</code>',
          '<code>src/assets/scss/theme/_mixed.scss</code>',
          '<code>src/assets/scss/theme/_theme.scss</code>',
          '<code>src/assets/scss/theme/_variables.scss</code>',
          '<code>src/assets/scss/theme/plugins/_full-calendar.scss</code>',
          '<code>src/features/e-commerce/ProductProvider.js</code>',
          '<code>src/features/e-commerce/QuantityController.js</code>',
          '<code>src/features/e-commerce/billing/Billing.js</code>',
          '<code>src/features/e-commerce/billing/BillingHeader.js</code>',
          '<code>src/features/e-commerce/checkout/Checkout.js</code>',
          '<code>src/features/e-commerce/checkout/OrderSummary.js</code>',
          '<code>src/features/events/event-detail/EventDetailsGmap.js</code>',
          '<code>src/features/social/feed/CreatePost.js</code>',
          '<code>src/features/social/feed/FeedCard.js</code>',
          '<code>src/features/social/feed/FeedContent.js</code>',
          '<code>src/components/common/FalconLink.js</code>',
          '<code>src/components/dashboards/analytics/active-users/ActiveUsers.js</code>',
          '<code>src/components/dashboards/analytics/audience/Audience.js</code>',
          '<code>src/components/dashboards/analytics/bounce-rate/BounceRate.js</code>',
          '<code>src/components/dashboards/analytics/index.js</code>',
          '<code>src/components/dashboards/analytics/top-pages/TopPages.js</code>',
          '<code>src/components/dashboards/default/ActiveUsers.js</code>',
          '<code>src/components/dashboards/default/BandwidthSaved.js</code>',
          '<code>src/components/dashboards/default/index.js</code>',
          '<code>src/components/dashboards/e-commerce/ProductShare/ProductShareChart.js</code>',
          '<code>src/components/dashboards/e-commerce/SalesPosLocation/SalesPosLocation.js</code>',
          '<code>src/components/dashboards/saas/DoMoreCard.js</code>',
          '<code>src/components/dashboards/saas/candle-chart/CandleChart.js</code>',
          '<code>src/components/dashboards/crm/ToDoList.js</code>',
          '<code>src/components/documentation/DarkMode.js</code>',
          '<code>src/components/navbar/top/NavbarTop.js</code>',
          '<code>src/components/navbar/vertical/NavbarVertical.js</code>',
          '<code>src/components/navbar/vertical/ToggleButton.js</code>',
          '<code>src/components/pages/associations/Associations.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/Account.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/Instructions.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/Liability.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/PrivacyPolicy.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/Rights.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/Rules.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/Subscription.js</code>',
          '<code>src/components/pages/miscellaneous/privacy-policy/Termination.js</code>',
          '<code>src/components/pages/user/profile/ActivityLog.js</code>',
          '<code>src/components/pages/user/profile/Events.js</code>',
          '<code>src/components/pages/user/profile/Followers.js</code>',
          '<code>src/components/pages/user/profile/Profile.js</code>',
          '<code>src/components/pages/user/settings/ExperienceForm.js</code>',
          '<code>src/components/pages/user/settings/ExperiencesSettings.js</code>',
          '<code>src/components/wizard/PersonalForm.js</code>',
          '<code>src/helpers/echart-utils.js</code>',
          '<code>src/helpers/initFA.js</code>',
          '<code>src/helpers/utils.js</code>',
          '<code>src/layouts/MainLayout.js</code>',
          '<code>src/layouts/MainRoutes.js</code>',
          '<code>src/routes/routes.js</code>',
          '<code>package.json</code>'
        ],
        Delete: ['<code>src/components/common/FalconVideoJS.js</code>']
      }
    }
  },
  {
    version: '3.0.0-beta2',
    title: 'Saker',
    publish: '17 November, 2021',
    logs: {
      New: [
        'Page : <code> E Commerce </code>',
        'Page : <code> Mangement </code>',
        'Page : <code> SaaS </code>',
        'Page : <code> Chat </code>',
        'Component : <code> AdvanceTable </code>',
        'Plugin : <code>react-textarea-autosize</code>',
        'Plugin : <code>react-leaflet</code>',
        'Plugin : <code>emoji-mart</code>',
        'Plugin : <code>react-chartjs-2</code>',
        'Plugin : <code>react-countup</code>'
      ],
      Update: [
        'Doc : Rename <code>src/components/bootstrap-components/AdvanceTable.js</code> to <code>src/components/bootstrap-components/AdvanceTableExamples.js</code>'
      ],

      Migration: {
        Add: [
          '<code>src/features/chat/**/*</code>',
          '<code>src/components/dashboards/e-commerce/**/*</code>',
          '<code>src/components/dashboards/project-management/**/*</code>',
          '<code>src/components/dashboards/saas/**/*</code>',
          '<code>src/data/chat/**/*</code>',
          '<code>src/data/dashboard/ecom.js</code>',
          '<code>src/data/dashboard/projectManagement.js</code>',
          '<code>src/data/dashboard/saas.js</code>',
          '<code>src/components/common/advance-table/AdvanceTable.js</code>',
          '<code>src/assets/scss/theme/plugins/_leaflet.scss</code>',
          '<code>src/assets/scss/theme/plugins/_emoji-mart.scss</code>'
        ],
        Update: [
          '<code>src/App.js</code>',
          '<code>src/config.js</code>',
          '<code>src/routes/routes.js</code>',
          '<code>src/layouts/MainRoutes.js</code>',
          '<code>src/helpers/utils.js</code>',
          '<code>src/helpers/initFA.js</code>',
          '<code>src/features/calendar/AddScheduleModal.js</code>',
          '<code>src/features/calendar/Calendar.js</code>',
          '<code>src/features/calendar/CalendarEventModal.js</code>',
          '<code>src/features/e-commerce/customers/Customers.js</code>',
          '<code>src/features/e-commerce/customers/CustomersTableHeader.js</code>',
          '<code>src/features/e-commerce/orders/order-list/Orders.js</code>',
          '<code>src/features/e-commerce/orders/order-list/OrdersTableHeader.js</code>',
          '<code>src/features/social/feed/FeedEvent.js</code>',
          '<code>src/components/common/Avatar.js</code>',
          '<code>src/components/common/BasicEChart.js</code>',
          '<code>src/components/common/CardDropdown.js</code>',
          '<code>src/components/common/FalconLink.js</code>',
          '<code>src/components/common/advance-table/AdvanceTableFooter.js</code>',
          '<code>src/components/common/advance-table/AdvanceTablePagination.js</code>',
          '<code>src/components/common/advance-table/AdvanceTableSearchBox.js</code>',
          '<code>src/components/common/advance-table/AdvanceTableWrapper.js</code>',
          '<code>src/components/dashboards/analytics/Intelligence.js</code>',
          '<code>src/components/dashboards/analytics/audience/Audience.js</code>',
          '<code>src/components/dashboards/analytics/bounce-rate/BounceRate.js</code>',
          '<code>src/components/dashboards/analytics/campaign-perfomance/CampaignPerfomance.js</code>',
          '<code>src/components/dashboards/analytics/session-by-browser/SessionByBrowser.js</code>',
          '<code>src/components/dashboards/analytics/top-pages/TopPages.js</code>',
          '<code>src/components/dashboards/analytics/traffic-source/TrafficSource.js</code>',
          '<code>src/components/dashboards/analytics/users-at-a-time/UsersAtTime.js</code>',
          '<code>src/components/dashboards/analytics/users-by-country/UsersByCountry.js</code>',
          '<code>src/components/dashboards/crm/LocationBySession/LocationBySession.js</code>',
          '<code>src/components/dashboards/crm/LocationBySession/LocationBySessionTable.js</code>',
          '<code>src/components/dashboards/crm/lead-conversation/LeadConversationChart.js</code>',
          '<code>src/components/dashboards/crm/recent-leads/RecentLeads.js</code>',
          '<code>src/components/dashboards/default/ActiveUsers.js</code>',
          '<code>src/components/dashboards/default/BandwidthSaved.js</code>',
          '<code>src/components/dashboards/default/MarketShare.js</code>',
          '<code>src/components/dashboards/default/SharedFiles.js</code>',
          '<code>src/components/dashboards/default/TopProducts.js</code>',
          '<code>src/components/dashboards/default/TotalOrder.js</code>',
          '<code>src/components/dashboards/default/WeeklySales.js</code>',
          '<code>src/components/dashboards/default/index.js</code>',
          '<code>src/components/documentation/Plugins.js</code>',
          '<code>src/data/common.js</code>',
          '<code>src/data/dashboard/analytics.js</code>',
          '<code>src/data/dashboard/default.js</code>',
          '<code>src/assets/scss/theme/_accordion.scss</code>',
          '<code>src/assets/scss/theme/_chat.scss</code>',
          '<code>src/assets/scss/theme/_forms.scss</code>',
          '<code>src/assets/scss/theme/_plugins.scss</code>',
          '<code>src/assets/scss/theme/_timeline.scss</code>',
          '<code>src/assets/scss/theme/plugins/_full-calendar.scss</code>'
        ],
        Delete: [
          '<code>src/components/dashboards/analytics/top-pages/TopPagesSearchBox.js</code>',
          '<code>src/components/dashboards/analytics/top-pages/TopPagesTable.js</code>',
          '<code>src/components/dashboards/crm/recent-leads/RecentLeadsTable.js</code>'
        ]
      }
    }
  },
  {
    version: '3.0.0-beta1',
    title: 'Merlin',
    publish: '31 October, 2021',
    logs: {
      New: [
        'Page : <code> Inbox </code>',
        'Page : <code> Email deatil </code>',
        'Page : <code> Compose </code>',
        'Page : <code> Product list </code>',
        'Page : <code> Product grid </code>',
        'Page : <code> Product details </code>',
        'Page : <code> Order list </code>',
        'Page : <code> Order details </code>',
        'Page : <code> Customers </code>',
        'Page : <code> Customer details </code>',
        'Page : <code> Shopping cart </code>',
        'Page : <code> Checkout </code>',
        'Page : <code> Billing </code>',
        'Page : <code> Invoice </code>',
        'Component : <code> CustomDateInput </code>',
        'Component : <code> MultiSelect </code>',
        'Component : <code> StarRating </code>',
        'Component : <code> TinymceEditor </code>',
        'Component : <code> Toast </code>',
        'Component : <code> AdvanceTable </code>',
        'Component : <code> AdvanceTableFooter </code>',
        'Component : <code> AdvanceTablePagination </code>',
        'Component : <code> AdvanceTableSearchBox </code>',
        'Component : <code> AdvanceTableWrapper </code>',
        'Component : <code> Googlemap </code>',
        'Doc : <code> Advance Select </code>',
        'Doc : <code> Editor </code>',
        'Doc : <code> Rating </code>',
        'Plugin : <code>@tinymce/tinymce-react</code>',
        'Plugin : <code>react-rating</code>',
        'Plugin : <code>google-maps-react</code>',
        'Plugin : <code>react-select</code>'
      ],
      Update: ['Bootstrap to v5.1.3', 'React-Bootstrap to v2.0.0'],

      Migration: {
        Add: [
          '<code>src/features/email/**/*</code>',
          '<code>src/features/events/**/*</code>',
          '<code>src/features/e-commerce/**/*</code>',
          '<code>src/components/bootstrap-components/AdvanceSelect.js</code>',
          '<code>src/components/bootstrap-components/Editor.js</code>',
          '<code>src/components/bootstrap-components/Rating.js</code>',
          '<code>src/components/map/Googlemap.js</code>',
          '<code>src/components/common/CustomDateInput.js</code>',
          '<code>src/components/common/MultiSelect.js</code>',
          '<code>src/components/common/StarRating.js</code>',
          '<code>src/components/common/TinymceEditor.js</code>',
          '<code>src/components/common/Toast.js</code>',
          '<code>src/components/common/advance-table/AdvanceTable.js</code>',
          '<code>src/components/common/advance-table/AdvanceTableFooter.js</code>',
          '<code>src/components/common/advance-table/AdvanceTablePagination.js</code>',
          '<code>src/components/common/advance-table/AdvanceTableSearchBox.js</code>',
          '<code>src/components/common/advance-table/AdvanceTableWrapper.js</code>',
          '<code>src/reducers/emailReducer.js</code>',
          '<code>src/reducers/productReducer.js</code>',
          '<code>src/hooks/useBulkSelect.js</code>',
          '<code>src/hooks/usePagination.js</code>',
          '<code>src/helpers/googleMapStyles.js</code>',
          '<code>src/data/events/timezones.js</code>',
          '<code>src/data/email/emails.js</code>',
          '<code>src/data/countries.js</code>',
          '<code>src/data/ecommerce/OrderDetailsData.js</code>',
          '<code>src/data/ecommerce/cartData.js</code>',
          '<code>src/data/ecommerce/checkoutData.js</code>',
          '<code>src/data/ecommerce/customerDetailsData.js</code>',
          '<code>src/data/ecommerce/customersData.js</code>',
          '<code>src/data/ecommerce/orderList.js</code>',
          '<code>src/data/ecommerce/productData.js</code>',
          '<code>src/data/socialIcons.js</code>',
          '<code>src/assets/scss/theme/plugins/_react-select.scss</code>',
          '<code>src/assets/scss/theme/plugins/_tinymce.scss</code>',
          '<code>src/assets/scss/theme/plugins/_toastify.scss</code>'
        ],
        Update: [
          '<code>src/App.js</code>',
          '<code>src/config.js</code>',
          '<code>src/routes/routes.js</code>',
          '<code>src/helpers/utils.js</code>',
          '<code>src/layouts/Layout.js</code>',
          '<code>src/layouts/MainLayout.js</code>',
          '<code>src/layouts/MainRoutes.js</code>',
          '<code>src/data/events/events.js</code>',
          '<code>src/data/footer/index.js</code>',
          '<code>src/components/bootstrap-components/AdvanceTable.js</code>',
          '<code>src/components/common/CardDropdown.js</code>',
          '<code>src/components/navbar/top/CartNotification.js</code>',
          '<code>src/components/navbar/vertical/NavbarVerticalMenu.js</code>',
          '<code>src/components/pages/faq/faq-alt/FaqAlt.js</code>',
          '<code>src/components/pages/landing/FooterStandard.js</code>',
          '<code>src/components/pages/user/profile/Events.js</code>',
          '<code>src/assets/scss/theme/_dashboard.scss </code>',
          '<code>src/assets/scss/theme/_mixed.scss </code>',
          '<code>src/assets/scss/theme/_plugins.scss </code>',
          '<code>src/assets/scss/theme/_reboot.scss </code>',
          '<code>src/assets/scss/theme/_variables.scss </code>',
          '<code>src/assets/scss/theme/root/_dark.scss </code>',
          '<code>src/assets/scss/theme/root/_light.scss </code>',
          '<code>package.json</code>'
        ],
        Delete: [
          '<code>src/data/footer/iconList.js</code>',
          '<code>src/components/common/AdvanceTable.js</code>'
        ]
      }
    }
  },
  {
    version: '3.0.0-alpha3',
    title: 'Grey kestrel',
    publish: '06 October, 2021',
    logs: {
      New: [
        'Page : <code> Crm </code>',
        'Page : <code> Calendar </code>',
        'Page : <code> Pricing default </code>',
        'Page : <code> Pricing alt </code>',
        'Page : <code> Faq basic </code>',
        'Page : <code> Faq alt </code>',
        'Page : <code> Faq accordion </code>',
        'Page : <code> Invite people </code>',
        'Page : <code> Privacy policy </code>',
        'Component : <code> DropdownFilter </code>',
        'Component : <code> IconItem </code>',
        'Plugin : <code>react-scrollspy</code>',
        'Plugin : <code>react-datepicker</code>',
        'Plugin : <code>@fullcalendar/bootstrap</code>',
        'Plugin : <code>@fullcalendar/daygrid</code>',
        'Plugin : <code>@fullcalendar/interaction</code>',
        'Plugin : <code>@fullcalendar/list</code>',
        'Plugin : <code>@fullcalendar/react</code>',
        'Plugin : <code>@fullcalendar/timegrid</code>',
        'Plugin : <code>@fullcalendar/daygrid</code>'
      ],
      Update: [
        'Doc : <code>Datepicker</code>',
        'Plugin : <strong>Removed</strong> <code>react-datetime</code>',
        'Component : Rename <code>UsersByCountryMap.js</code> to <code>WorldMap.js</code>'
      ],

      Migration: {
        Add: [
          '<code>src/components/dashboards/crm/**/*</code>',
          '<code>src/features/calendar/**/*</code>',
          '<code>src/components/pages/faq/**/*</code>',
          '<code>src/components/pages/miscellaneous/**/*</code>',
          '<code>src/components/pages/pricing/**/*</code>',
          '<code>src/components/common/icon/IconItem.js</code>',
          '<code>src/components/common/DropdownFilter.js</code>',
          '<code>src/data/calendar/events.js</code>',
          '<code>src/data/dashboard/crm.js</code>',
          '<code>src/data/faqs.js</code>',
          '<code>src/data/pricing.js</code>',
          '<code>src/hooks/useBreakpoints.js</code>',
          '<code>src/assets/scss/theme/plugins/_full-calendar.scss</code>',
          '<code>src/assets/scss/theme/plugins/_react-datepicker.scss</code>'
        ],
        Update: [
          '<code>src/assets/scss/theme/_dashboard.scss </code>',
          '<code>src/assets/scss/theme/_mixed.scss </code>',
          '<code>src/assets/scss/theme/_plugins.scss</code>',
          '<code>src/assets/scss/theme/_variables.scss</code>',
          '<code>src/assets/scss/theme/plugins/_echarts.scss</code>',
          '<code>src/assets/scss/theme/root/_dark.scss</code>',
          '<code>src/assets/scss/theme/root/_light.scss</code>',
          '<code>src/components/bootstrap-components/DatePicker.js</code>',
          '<code>src/components/common/CardDropdown.js</code>',
          '<code>src/components/common/PageHeader.js</code>',
          '<code>src/components/common/icon/IconGroup.js</code>',
          '<code>src/components/documentation/GettingStarted.js</code>',
          '<code>src/components/documentation/Plugins.js</code>',
          '<code>src/components/documentation/change-log/changelogs.js</code>',
          '<code>src/components/navbar/top/NavbarNavLink.js</code>',
          '<code>src/components/navbar/top/NavbarTopDropDownMenus.js</code>',
          '<code>src/components/navbar/vertical/NavbarVertical.js</code>',
          '<code>src/components/navbar/vertical/NavbarVerticalMenu.js</code>',
          '<code>src/components/pages/user/InputField.js</code>',
          '<code>src/components/wizard/BillingForm.js</code>',
          '<code>src/components/wizard/PersonalForm.js</code>',
          '<code>src/components/wizard/WizardInput.js</code>',
          '<code>src/config.js</code>',
          '<code>src/helpers/echart-utils.js</code>',
          '<code>src/helpers/initFA.js</code>',
          '<code>src/helpers/utils.js</code>',
          '<code>src/layouts/MainRoutes.js</code>',
          '<code>src/routes/routes.js</code>'
        ]
      }
    }
  },
  {
    version: '3.0.0-alpha2',
    title: "Dickinson's kestrel",
    publish: '07 September, 2021',
    logs: {
      New: [
        'Page : <code> Analytics </code>',
        'Page : <code> Feed </code>',
        'Page : <code> Activity Log </code>',
        'Page : <code> Notifications </code>',
        'Page : <code> Followers </code>',
        'Page : <code> Profile </code>',
        'Page : <code> Settings </code>',
        'Page : <code> Associations </code>',
        'Page : <code> Advance tables </code>',
        'Page : <code> Lightbox </code>',
        'Page : <code> Placeholder </code>',
        'Plugin : react-table',
        'Plugin : react-image-lightbox',
        'Plugin : video.js'
      ],
      Update: ['Bootstrap to v5.1.0', 'React-Bootstrap to v2.0.0-beta.6'],
      Migration: {
        Add: [
          '<code>src/components/dashboards/analytics/**/*</code>',
          '<code>src/features/**/*</code>',
          '<code>src/components/pages/user/**/*</code>',
          '<code>src/components/pages/associations/*</code>',
          '<code>src/components/notification/*</code>',
          '<code>src/components/common/SubtleBadge.js</code>',
          '<code>src/components/common/FalconLink.js</code>',
          '<code>src/components/bootstrap-components/Placeholder.js</code>',
          '<code>src/components/bootstrap-components/Lightbox.js</code>',
          '<code>src/components/common/AdvanceTable.js</code>',
          '<code>src/components/bootstrap-components/AdvanceTable.js</code>',
          '<code>src/assets/json/world.json</code>',
          '<code>src/assets/scss/theme/plugins/_echarts.scss</code>',
          '<code>src/assets/scss/theme/plugins/_react-table.scss</code>',
          '<code>src/helpers/echart-utils.js</code>',
          '<code>src/reducers/feedReducer.js</code>'
        ],
        Update: [
          '<code> src/components/navbar/top/ProfileDropdown.js </code>',
          '<code> src/components/bootstrap-components/Badges.js </code>',
          '<code> src/components/bootstrap-components/ListGroup.js </code>',
          '<code> src/components/bootstrap-components/Tables.js </code>',
          '<code> src/components/dashboards/default/RunningProjects.js </code>',
          '<code> src/components/dashboards/default/WeeklySales.js </code>',
          '<code> src/components/navbar/top/SearchBox.js </code>',
          '<code> src/components/navbar/vertical/NavbarVerticalMenuItem.js</code>',
          '<code> src/components/pages/ComingSoon.js </code>',
          
          '<code> src/components/bootstrap-components/FormValidation.js </code>',
          '<code> src/helpers/initFA.js </code>',
          '<code> src/assets/scss/_bootstrap.scss </code>',
          '<code> src/assets/scss/theme/_variables.scss </code>',
          '<code> src/assets/scss/theme/_avatar.scss </code>',
          '<code> src/assets/scss/theme/_functions.scss </code>',
          '<code> src/assets/scss/theme/_popover.scss </code>',
          '<code> src/assets/scss/theme/_popover.scss </code>',
          '<code> src/assets/scss/theme/_dashboard.scss </code>',
          '<code> src/assets/scss/theme/_plugins.scss </code>',
          '<code> src/assets/scss/theme/_table.scss </code>',
          '<code> src/assets/scss/theme/mixins/_utilities.scss </code>'
        ]
      },
      Fix: ['Authentication Modal issue']
    }
  },
  {
    version: '3.0.0-alpha1',
    title: 'initial release',
    publish: '12 August, 2021',
    children: 'Nothing to see here.'
  }
];
