import htmx from 'htmx.org'

htmx.config.indicatorClass = "loading"


// Mobile menu toggle
const menuToggle = document.getElementById('menu-toggle');
const mobileMenu = document.getElementById('mobile-menu');

menuToggle.addEventListener('click', () => {
  mobileMenu.classList.toggle('hidden');
});




const toggleDarkMode = () => {
  const htmlElement = document.documentElement;
  const isDarkMode = htmlElement.classList.toggle('dark');
  localStorage.setItem('theme', isDarkMode ? 'dark' : 'light');

  // Update the position of all switch indicators
  document.querySelectorAll('.switch-indicator').forEach((indicator) => {
    indicator.style.transform = isDarkMode ? 'translateX(44px)' : 'translateX(0)';
  });
};

// Initialize on DOMContentLoaded
document.addEventListener('DOMContentLoaded', () => {
  const storedTheme = localStorage.getItem('theme');
  const isDarkMode = storedTheme === 'dark';

  if (isDarkMode) {
    document.documentElement.classList.add('dark');
  }

  // Update the position of all switch indicators
  document.querySelectorAll('.switch-indicator').forEach((indicator) => {
    indicator.style.transform = isDarkMode ? 'translateX(48px)' : 'translateX(0)';
  });

  // Attach event listeners to all theme switchers
  document.querySelectorAll('.theme-switcher').forEach((switcher) => {
    switcher.addEventListener('click', toggleDarkMode);
  });
});
