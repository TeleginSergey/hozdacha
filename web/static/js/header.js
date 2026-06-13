// Хедер: авто-определение активной вкладки + аватар пользователя.
// Подключается на всех страницах, где есть #siteMainNav.
(function() {
    'use strict';

    function highlightActiveNav() {
        const path = (window.location && window.location.pathname) || '/';
        const nav = document.getElementById('siteMainNav');
        if (!nav) return;

        // Снимаем .active со всех ссылок в навигации.
        nav.querySelectorAll('a').forEach(function(a) { a.classList.remove('active'); });

        // Подбираем подходящий data-nav по текущему пути.
        let active = null;
        if (path === '/' || path === '') {
            active = nav.querySelector('a[data-nav="home"]');
        } else if (path.indexOf('/catalog') === 0) {
            active = nav.querySelector('a[data-nav="catalog"]');
        } else if (path.indexOf('/promotions') === 0) {
            active = nav.querySelector('a[data-nav="promotions"]');
        } else if (path.indexOf('/cart') === 0) {
            active = nav.querySelector('a[data-nav="cart"]');
        }

        if (active) active.classList.add('active');
    }

    function refreshUserArea() {
        const token = localStorage.getItem('auth_token');
        const user = JSON.parse(localStorage.getItem('user') || 'null');
        const authButtons = document.getElementById('authButtons');
        const userMenu = document.getElementById('userMenu');
        const avatar = document.getElementById('avatarLetter');
        const greeting = document.getElementById('userGreeting');
        if (!authButtons || !userMenu) return;

        if (token && user) {
            authButtons.style.display = 'none';
            userMenu.style.display = 'flex';
            const name = user.full_name || user.username || user.email || 'U';
            if (avatar) avatar.textContent = name.charAt(0).toUpperCase();
            if (greeting) greeting.textContent = name;
        } else {
            authButtons.style.display = 'flex';
            userMenu.style.display = 'none';
        }
    }

    function refreshCartBadge() {
        const cart = JSON.parse(localStorage.getItem('cart') || '[]');
        const count = cart.reduce(function(s, i) { return s + (parseInt(i.quantity, 10) || 0); }, 0);
        const badge = document.getElementById('headerCartCount');
        if (!badge) return;
        if (count > 0) {
            badge.textContent = count;
            badge.style.display = 'grid';
        } else {
            badge.style.display = 'none';
        }
    }

    document.addEventListener('DOMContentLoaded', function() {
        highlightActiveNav();
        refreshUserArea();
        refreshCartBadge();
    });

    // Слушаем изменения localStorage между вкладками.
    window.addEventListener('storage', function() {
        refreshUserArea();
        refreshCartBadge();
    });
})();
