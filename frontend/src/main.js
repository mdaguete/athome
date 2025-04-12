import './style.css';
import { BlueskyAPI } from './js/api';
import { BlueskyClient } from './js/client';

// Initialize the client immediately since Vite handles module loading
const client = new BlueskyClient();
const api = new BlueskyAPI();

// Get the default handle from the HTML data attribute
const defaultHandle = document.documentElement.dataset.defaultHandle;

// Initialize navigation
function initializeNavigation() {
  const navLinks = document.querySelectorAll('.nav-link');
  const sections = document.querySelectorAll('.content-section');

  navLinks.forEach(link => {
    link.addEventListener('click', (e) => {
      e.preventDefault();

      // Remove active class from all links and sections
      navLinks.forEach(l => l.classList.remove('active'));
      sections.forEach(s => s.classList.remove('active'));

      // Add active class to clicked link and corresponding section
      link.classList.add('active');
      const sectionId = link.dataset.section;
      document.getElementById(sectionId).classList.add('active');

      // Load portfolio data if switching to portfolio section
      if (sectionId === 'portfolio') {
        loadPortfolio(defaultHandle);
      }
    });
  });
}

// Load portfolio data
async function loadPortfolio(handle) {
  const portfolioItems = document.getElementById('portfolio-items');
  const loading = document.getElementById('loading');

  try {
    // First check if portfolio feature is enabled
    const config = await api.getPortfolioConfig();
    if (!config.enabled) {
      portfolioItems.innerHTML = '<div class="portfolio-message">Portfolio feature is not enabled</div>';
      return;
    }

    loading.style.display = 'block';
    const portfolio = await api.getPortfolio(handle);

    // Clear existing items
    portfolioItems.innerHTML = '';

    // Check if portfolio data exists and has the expected structure
    if (!portfolio || !portfolio.projects) {
      portfolioItems.innerHTML = '<div class="portfolio-message">No portfolio items available</div>';
      return;
    }

    // Create portfolio items
    portfolio.projects.forEach(item => {
      const itemElement = document.createElement('div');
      itemElement.className = 'portfolio-item';

      const date = item.createdAt ? new Date(item.createdAt).toLocaleDateString() : '';

      itemElement.innerHTML = `
                ${item.imageURL ? `<img src="${item.imageURL}" alt="${item.title}" class="portfolio-image">` : ''}
                <div class="portfolio-content">
                    <h3 class="portfolio-title">${item.title || ''}</h3>
                    <p class="portfolio-description">${item.description || ''}</p>
                    ${item.URL ? `<a href="${item.URL}" target="_blank" rel="noopener noreferrer" class="portfolio-link">View Project</a>` : ''}
                    ${date ? `<div class="portfolio-date">${date}</div>` : ''}
                </div>
            `;

      portfolioItems.appendChild(itemElement);
    });

    // If no items were added, show a message
    if (portfolioItems.children.length === 0) {
      portfolioItems.innerHTML = '<div class="portfolio-message">No portfolio items available</div>';
    }
  } catch (error) {
    console.error('Error loading portfolio:', error);
    portfolioItems.innerHTML = '<div class="portfolio-message">Failed to load portfolio</div>';
  } finally {
    loading.style.display = 'none';
  }
}

// Initialize the app
async function initializeApp() {
  initializeNavigation();
  await client.initialize(defaultHandle);
}

// Start the app
initializeApp().catch(console.error);
