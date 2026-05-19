// Главная: хиты продаж + акции

(function () {
    const { escapeHtml, formatPrice, Cart, PLACEHOLDER_IMG } = App;

    function productCard(p) {
        const id = p.ID || p.id;
        const name = p.Name || p.name || '';
        const price = p.Price || p.price || 0;
        const stock = p.Stock || p.stock || 0;
        const image = p.ImageURL || p.image_url || '';
        const stockClass = stock <= 0 ? 'stock-out' : stock < 5 ? 'stock-low' : 'stock-ok';
        const stockText = stock <= 0 ? 'Нет в наличии' : stock < 5 ? `Осталось ${stock} шт.` : `В наличии`;
        return `
            <a href="/product/${id}" class="product-card">
                <img class="product-card-img" src="${image ? escapeHtml(image) : PLACEHOLDER_IMG}" alt="${escapeHtml(name)}" onerror="this.src='${PLACEHOLDER_IMG}'">
                <div class="product-card-body">
                    <h3>${escapeHtml(name)}</h3>
                    <div class="product-card-price">${formatPrice(price)}</div>
                    <div class="product-card-stock ${stockClass}">${stockText}</div>
                    <button class="btn btn-primary btn-sm" data-add-id="${id}" data-add-name="${escapeHtml(name)}" data-add-price="${price}" data-add-stock="${stock}" data-add-image="${escapeHtml(image)}" ${stock <= 0 ? 'disabled' : ''}>В корзину</button>
                </div>
            </a>
        `;
    }

    function bindCardButtons(container) {
        container.querySelectorAll('[data-add-id]').forEach((btn) => {
            btn.addEventListener('click', (e) => {
                e.preventDefault();
                e.stopPropagation();
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

    async function loadFeatured() {
        const grid = document.getElementById('featuredGrid');
        try {
            const res = await fetch('/api/products?limit=8');
            const data = await res.json();
            const products = data.products || [];
            if (products.length === 0) {
                grid.innerHTML = '<p style="grid-column:1/-1;text-align:center;color:var(--gray-500)">Товаров пока нет</p>';
                return;
            }
            grid.innerHTML = products.map(productCard).join('');
            bindCardButtons(grid);
        } catch {
            grid.innerHTML = '<p style="grid-column:1/-1;text-align:center;color:var(--red)">Ошибка загрузки</p>';
        }
    }

    async function loadPromotions() {
        const grid = document.getElementById('promotionsGrid');
        try {
            const res = await fetch('/api/promotions');
            const data = await res.json();
            const promos = data.promotions || [];
            if (promos.length === 0) {
                grid.innerHTML = '<p style="grid-column:1/-1;text-align:center;color:var(--gray-500)">Акций пока нет — скоро появятся!</p>';
                return;
            }
            grid.innerHTML = promos.map((p) => {
                const title = App.escapeHtml(p.promotions_title || p.title || '');
                const desc = App.escapeHtml(p.promotions_description || p.description || '');
                const image = p.promotions_image_url || p.image_url || '';
                const discount = p.promotions_discount || p.discount || 0;
                return `
                    <div class="product-card">
                        <img class="product-card-img" src="${image ? App.escapeHtml(image) : App.PLACEHOLDER_IMG}" onerror="this.src='${App.PLACEHOLDER_IMG}'">
                        <div class="product-card-body">
                            <h3>${title}</h3>
                            ${discount > 0 ? `<div class="product-card-price" style="color:var(--amber)">−${discount}%</div>` : ''}
                            <p style="font-size:0.85rem;color:var(--gray-600)">${desc}</p>
                        </div>
                    </div>
                `;
            }).join('');
        } catch {
            grid.innerHTML = '<p style="grid-column:1/-1;text-align:center;color:var(--red)">Ошибка загрузки</p>';
        }
    }

    document.addEventListener('DOMContentLoaded', () => {
        loadFeatured();
        loadPromotions();
    });
})();
