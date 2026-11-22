import base from './playwright.config';
export default { ...base, use: { ...base.use, launchOptions: { executablePath: process.env.CHROME_BIN || '/usr/bin/chromium', args: ['--no-sandbox','--disable-dev-shm-usage'] } } };
