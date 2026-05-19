// Каталог: список товаров, поиск, пагинация

(function () {
    const { escapeHtml, formatPrice, Cart, PLACEHOLDER_IMG } = App;
    const PER_PAGE = 20;

    const state = {
        page: 1,
        query: new URLSearchParams(window.location.search).get('q') || '',
        total: 0
    };

    function productCard(p) {
        const id = p.ID || p.id;
        const name = p.Name || p.name || '';
        const price = p.Price || p.price || 0;
        const stock = p.Stock || p.stock || 0;
        const image = p.ImageURL || p.image_url || '';
        const stockClass = stock <= 0 ? 'stock-out' : stock < 5 ? 'stock-low' : 'stock-ok';
        const stockText = stock <= 0 ? 'Нет в наличии' : stock < 5 ? `Осталось ${stock} шт.` : 'В наличии';
        return `
            <div class="product-card">
                <a href="/product/${id}">
                    <img class="product-card-img" src="${image ? escapeHtml(image) : PLACEHOLDER_IMG}" alt="${escapeHtml(name)}" onerror="this.src='${PLACEHOLDER_IMG}'">
                </a>
                <div class="product-card-body">
                    <h3><a href="/product/${id}" style="color:inherit">${escapeHtml(name)}</a></h3>
                    <div class="product-card-price">${formatPrice(price)}</div>
                    <div class="product-card-stock ${stockClass}">${stockText}</div>
                    <button class="btn btn-primary btn-sm"
                        data-add-id="${id}"
                        data-add-name="${escapeHtml(name)}"
                        data-add-price="${price}"
                        data-add-stock="${stock}"
                        data-add-image="${escapeHtml(image)}"
                        ${stock <= 0 ? 'disabled' : ''}>${stock <= 0 ? 'Нет в наличии' : 'В корзину'}</button>
                </div>
            </div>
        `;
    }

    function bindCards(container) {
        container.querySelectorAll('[data-add-id]').forEach((btn) => {
            btn.addEventListener('click', (e) => {
                e.preventDefault();
                Cart.add({
                    id: btn.dataset.addId,
                    name: btn.dataset.addName,
                    price: btn.dataset.addPrice,
                    stock: btn.dataset.addStock,
                    image: btn.dataset.addImage
                });
            });
        });
    }

    function renderPagination() {
        const totalPages = Math.max(1, Math.ceil(state.total / PER_PAGE));
        const wrap = document.getElementById('pagination');
        if (totalPages <= 1) { wrap.innerHTML = ''; return; }
        const cur = state.page;
        const buttons = [];
        const mk = (label, page, opts = {}) => {
            const cls = opts.active ? 'active' : '';
            const dis = opts.disabled ? 'disabled' : '';
            return `<button class="${cls}" data-page="${page}" ${dis}>${label}</button>`;
        };
        buttons.push(mk('‹', cur - 1, { disabled: cur === 1 }));
        const showRange = (from, to) => { for (let i = from; i <= to; i++) buttons.push(mk(i, i, { active: i === cur })); };
        if (totalPages <= 7) showRange(1, totalPages);
        else {
            buttons.push(mk(1, 1, { active: cur === 1 }));
            if (cur > 3) buttons.push('<span class="ellipsis">…</span>');
            const from = Math.max(2, cur - 1);
            const to = Math.min(totalPages - 1, cur + 1);
            showRange(from, to);
            if (cur < totalPages - 2) buttons.push('<span class="ellipsis">…</span>');
            buttons.push(mk(totalPages, totalPages, { active: cur === totalPages }));
        }
        buttons.push(mk('›', cur + 1, { disabled: cur === totalPages }));
        wrap.innerHTML = buttons.join('');
        wrap.querySelectorAll('button[data-page]').forEach((b) => {
            b.addEventListener('click', () => {
                const p = Number(b.dataset.page);
                if (p >= 1 && p <= totalPages && p !== cur) {
                    state.page = p;
                    load();
                    window.scrollTo({ top: 0, behavior: 'smooth' });
                }
            });
        });
    }

    async function load() {
        const grid = document.getElementById('productsGrid');
        grid.innerHTML = '<p style="grid-column:1/-1;text-align:center;color:var(--gray-500);padding:40px">Загрузка...</p>';
        const offset = (state.page - 1) * PER_PAGE;
        const url = state.query
            ? `/api/products/search?q=${encodeURIComponent(state.query)}&limit=${PER_PAGE}&offset=${offset}`
            : `/api/products?limit=${PER_PAGE}&offset=${offset}`;
        try {
            const res = await fetch(url);
            const data = await res.json();
            const products = data.products || [];
            state.total = data.total || products.length;
            if (products.length === 0) {
                grid.innerHTML = `<p style="grid-column:1/-1;text-align:center;color:var(--gray-500);padding:60px">Ничего не найдено${state.query ? ' по запросу «' + escapeHtml(state.query) + '»' : ''}</p>`;
                document.getElementById('pagination').innerHTML = '';
                return;
            }
            grid.innerHTML = products.map(productCard).join('');
            bindCards(grid);
            renderPagination();
        } catch (err) {
            grid.innerHTML = '<p style="grid-column:1/-1;text-align:center;color:var(--red);padding:40px">Ошибка загрузки</p>';
        }
    }

    document.addEventListener('DOMContentLoaded', () => {
        const input = document.getElementById('searchInput');
        input.value = state.query;
        const doSearch = () => { state.query = input.value.trim(); state.page = 1; load(); };
        document.getElementById('searchBtn').addEventListener('click', doSearch);
        input.addEventListener('keydown', (e) => { if (e.key === 'Enter') doSearch(); });
        load();
    });
})();
