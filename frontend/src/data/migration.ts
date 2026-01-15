export const removeCode = `npm uninstall react-scripts webpack webpack-cli webpack-fix-style-only-entries`;
export const viteConfigCode = `
import { defineConfig, loadEnv } from 'vite';
import fs from 'fs/promises';
import react from '@vitejs/plugin-react';
import jsconfigPaths from 'vite-jsconfig-paths';
import compileSCSS from './compile-scss';

export default ({ mode }) => {
  process.env = { ...process.env, ...loadEnv(mode, process.cwd()) };

  return defineConfig({
    plugins: [react(), jsconfigPaths(), compileSCSS()],
    base: process.env.VITE_PUBLIC_URL || '/',
    esbuild: {
      loader: 'jsx',
      include: /src\\/.*\\.jsx?$/,
      exclude: []
    },
    optimizeDeps: {
      esbuildOptions: {
        plugins: [
          {
            name: 'load-js-files-as-jsx',
            setup(build) {
              build.onLoad({ filter: /src\\/.*\\.js$/ }, async args => ({
                loader: 'jsx',
                contents: await fs.readFile(args.path, 'utf8')
              }));
            }
          }
        ]
      }
    },
    define: {
      global: 'window'
    },
    server: {
      open: true,
      port: Number(process.env.VITE_APP_PORT) || 3000,
      host: process.env.VITE_APP_HOST || 'localhost'
    }
  });
};
`;

export const compileSCSSCode = `
/* eslint-disable */

import path from 'path';
import fs from 'fs';
import * as sass from 'sass';
import rtlcss from 'rtlcss';

const compileSCSS = () => ({
  name: 'compile-scss',
  configureServer(server) {
    const scssWatcher = server.watcher;
    const scssGlob = path.resolve(__dirname, 'src/assets/scss/**/*.scss');
    scssWatcher.add(scssGlob);

    const scssFiles = [path.resolve(__dirname, 'src/assets/scss/theme.scss')];

    const compileSCSSToCSS = async file => {
      const result = await sass.compileAsync(file, { style: 'expanded' });
      const fileName = path.basename(file, path.extname(file));

      const cssPath = path.resolve(__dirname, \`public/css/\${fileName}.css\`);
      fs.mkdirSync(path.dirname(cssPath), { recursive: true });
      fs.writeFileSync(cssPath, result.css);

      const rtlResult = rtlcss.process(result.css);
      const rtlCssPath = path.resolve(
        __dirname,
        \`public/css/\${fileName}.rtl.css\`
      );
      fs.writeFileSync(rtlCssPath, rtlResult);
    };

    scssWatcher.on('change', file => {
      if (file.endsWith('.scss')) {
        scssFiles.map(file => {
          compileSCSSToCSS(file);
        });
      }
    });

    scssFiles.map(file => {
      compileSCSSToCSS(file);
    });
  },
  handleHotUpdate({ file, server }) {
    server.ws.send({
      type: 'full-reload'
    });
  }
});

export default compileSCSS;
`;

export const useToggleStyleCode = `
  import { useEffect, useState } from 'react';
  
  const useToggleStylesheet = (isRTL, isDark) => {
    const [isLoaded, setIsLoaded] = useState(false);
    const publicUrl = import.meta.env.VITE_PUBLIC_URL;
  
    useEffect(() => {
      setIsLoaded(false);
      Array.from(document.getElementsByClassName('theme-stylesheet')).forEach(
        link => link.remove()
      );
      const link = document.createElement('link');
      link.href = \`\${publicUrl}css/theme\${isRTL ? '.rtl' : ''}.css\`;
      link.type = 'text/css';
      link.rel = 'stylesheet';
      link.className = 'theme-stylesheet';
  
      const userLink = document.createElement('link');
      userLink.href = \`\${publicUrl}css/user\${isRTL ? '.rtl' : ''}.css\`;
      userLink.type = 'text/css';
      userLink.rel = 'stylesheet';
      userLink.className = 'theme-stylesheet';
  
      link.onload = () => {
        setIsLoaded(true);
      };
  
      document.getElementsByTagName('head')[0].appendChild(link);
      document.getElementsByTagName('head')[0].appendChild(userLink);
      document
        .getElementsByTagName('html')[0]
        .setAttribute('dir', isRTL ? 'rtl' : 'ltr');
    }, [isRTL]);
  
    useEffect(() => {
      document.documentElement.setAttribute(
        'data-bs-theme',
        isDark ? 'dark' : 'light'
      );
    }, []);
  
    return { isLoaded };
  };
  
  export default useToggleStylesheet;
`;

export const jsonScriptCode = `{
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "serve": "vite preview"
  }
}`;

export const jsonScriptRemoveCode = `{
  "scripts": {
    "start": "react-scripts start",
    "build": "react-scripts build",
    "build:css": "webpack --config webpack.config.js",
    "watch:css": "webpack --config webpack.config.js --watch"
  }
}`;

export const envCode = `
  VITE_ESLINT_NO_DEV_ERRORS
  VITE_PUBLIC_URL
  VITE_SKIP_PREFLIGHT_CHECK
  VITE_REACT_APP_TINYMCE_APIKEY
  VITE_REACT_APP_GOOGLE_API_KEY
`;

export const indexHTMLCode = `
  <!DOCTYPE html>
  <html lang="en" dir="ltr">
    <head>
      <meta charset="utf-8" />
      <link rel="shortcut icon" href="/src/assets/img/favicons/favicon.ico" />
      <meta name="viewport" content="width=device-width, initial-scale=1" />
      <meta name="theme-color" content="#2c7be5" />
      <link rel="manifest" href="/manifest.json" />
      <link
        rel="stylesheet"
        href="https://fonts.googleapis.com/css?family=Open+Sans:300,400,500,600,700|Poppins:100,200,300,400,500,600,700,800,900&display=swap"
      />

      <title>Falcon React | ReactJS Dashboard & WebApp Template</title>
    </head>

    <body>
      <noscript>You need to enable JavaScript to run this app.</noscript>
      <main class="main" id="main"></main>
      <script type="module" src="/src/index.jsx"></script>
    </body>
  </html>
`;

export const renameJsToJsxCode = `
import fs from 'fs';
import path from 'path';

const pathName = [
  'src/components',
  'src/layouts',
  'src/hooks',
  'src/providers',
  'src/widgets',
  'src/App.js',
  'src/index.js'
];

// Main function to rename .js files to .jsx
// This function takes an array of file and folder paths as input
// It renames all .js files to .jsx in the specified paths

function renameJsToJsx(inputs) {
  inputs.forEach(input => {
    const inputPath = path.resolve(input);

    if (!fs.existsSync(inputPath)) {
      console.log(\`Not found: \${inputPath}\`);
      return;
    }

    const stats = fs.statSync(inputPath);

    if (stats.isDirectory()) {
      traverseAndRename(inputPath);
    } else if (stats.isFile() && path.extname(inputPath) === '.js') {
      renameFile(inputPath);
    } else {
      console.log(\`Skipping: \${inputPath} (Not a .js file or folder)\`);
    }
  });
}

// Helper function to traverse directories and rename .js files to .jsx
// This function uses recursion to go through all subdirectories
// and rename any .js files it finds

function traverseAndRename(currentPath) {
  const items = fs.readdirSync(currentPath);

  items.forEach(item => {
    const itemPath = path.join(currentPath, item);
    const stats = fs.statSync(itemPath);

    if (stats.isDirectory()) {
      traverseAndRename(itemPath);
    } else if (stats.isFile() && path.extname(itemPath) === '.js') {
      renameFile(itemPath);
    }
  });
}

// Helper function to rename a single .js file to .jsx

function renameFile(filePath) {
  const newFilePath = path.join(
    path.dirname(filePath),
    path.basename(filePath, '.js') + '.jsx'
  );
  fs.renameSync(filePath, newFilePath);
  console.log(\`Renamed: \${filePath} → \${newFilePath}\`);
}

// File and folder paths to rename
// You can add more paths to this array as needed
// Note: The paths should be relative to the current working directory
renameJsToJsx(pathName);

`;

export const viteInstallCode = `
npm install vite vite-jsconfig-paths @vitejs/plugin-react --save-dev
npm install rtlcss
`;

export const paths = `
const paths = [
  'src/components',
  'src/layouts',
  'src/hooks',
  'src/providers',
  'src/widgets',
  'src/App.js',
  'src/index.js'
];`;

export const forwardRefCode = `
  const MyComponent = ({ ref, ...props }) => {
    return (
      <div ref={ref} {...props}>
        {/* Your component content */}
      </div>
    );
  };

  // Usage 
  const App = () => {
    const myRef = useRef(null);
    return (
      <MyComponent ref={myRef} />
    );
  };
`;

export const useContextCode = `
  import { use } from 'react';
  import { AppContext } from 'providers/AppProvider';

  const MyComponent = () => {
    - const { config } = useContext(AppContext);
    + const { config } = use(AppContext);
    const { theme } = config;
    return (
      <div>
        {/* Use config or other context values */}
        <p>Current theme: {theme}</p>
      </div>
    );
  };
`;

export const contextProviderCode = `
  const themeContext = createContext('');

  const App = ({children}) => {
    return (
      - <themeContext.Provider value="dark">
      +  <themeContext value="dark">
          {children}
      + </themeContext>
      - </themeContext.Provider>
      )
  }
`;
