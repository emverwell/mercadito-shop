// Keeps the cart badge in the header up to date.
(function () {
  var badge = document.getElementById('cart-count');
  if (!badge) return;
  fetch('/api/cart-count', { credentials: 'same-origin' })
    .then(function (r) { return r.json(); })
    .then(function (d) { badge.textContent = d.count; })
    .catch(function () {});
})();
