# E-shop — a showcase pack: how to run an online store on kalita.
# Demonstrates a product catalog, a master-detail order (lines roll up into the
# order total via aggregates), an order fulfilment workflow, HITL on refunds,
# a customer portal scoped by ABAC, and revenue dashboards. English reference.

# --- catalog -----------------------------------------------------------------

entity Product "Product":
    sku:       string required unique label="SKU"
    name:      string required label="Name"
    category:  enum[Electronics, Apparel, Home, Books, Other] default=Other label="Category"
    price:     money label="Price"
    in_stock:  int default=0 label="In stock"
    published: bool default=false label="Published"

entity Customer "Customer":
    name:  string required label="Name"
    email: email label="Email"
    phone: phone label="Phone"
    user:  ref[core.User] label="Portal login"

# --- orders (master-detail) --------------------------------------------------

entity Order "Order":
    number:     serial format="ORD-{year}-{seq:6}" label="Number"
    customer:   ref[Customer] on_delete=restrict label="Customer"
    placed_at:  datetime default=$now label="Placed"
    line_count: int   computed = count(OrderLine where order = $self) label="Items"
    # the order total rolls up the computed line totals of its lines
    total:      money computed = sum(OrderLine.line_total where order = $self) label="Total"
    status:     enum[Cart, Placed, Paid, Packing, Shipped, Delivered, Cancelled] default=Cart label="Status"

entity OrderLine "Order line":
    order:      ref[Order] on_delete=cascade label="Order"
    product:    ref[Product] on_delete=restrict label="Product"
    qty:        int default=1 label="Qty"
    unit_price: money label="Unit price"
    line_total: money computed = unit_price * qty label="Line total"

workflow Order on status:
    Cart    -> Placed:    place label="Place order"
    Placed  -> Paid:      pay label="Mark paid"
    Paid    -> Packing:   pack label="Pack"
    Packing -> Shipped:   ship label="Ship"
    Shipped -> Delivered: deliver label="Delivered"
    Cart    -> Cancelled: cancel label="Cancel"
    # refunding a paid order needs the store manager's signature
    Paid    -> Cancelled: refund requires approval(StoreManager) label="Refund"

# --- roles & permissions -----------------------------------------------------

roles:
    StoreManager
    Clerk
    Customer

permissions:
    StoreManager:
        full    [Product, Customer, Order, OrderLine]
        approve [refund]
        act     [place, pay, pack, ship, deliver, cancel, refund]
    # a clerk runs the catalog and fulfils orders, but cannot sign refunds
    Clerk:
        full [Product, Customer, Order, OrderLine]
        act  [place, pay, pack, ship, deliver, cancel]
    # a customer sees only their own orders (ABAC ref-path through the portal login)
    Customer:
        read Order where customer.user = $me
        read [Product, OrderLine]

# --- automation --------------------------------------------------------------

automation:
    on stuck Order in Paid for 2d:
        escalate_to StoreManager

# --- dashboards ---------------------------------------------------------------

dashboard StoreBoard "Store overview":
    tile "Open orders":     count Order where status != Delivered and status != Cancelled
    tile "Awaiting payment": count Order where status = Placed
    tile "To ship":         count Order where status = Paid or status = Packing
    tile "Orders by status": count Order group by status

dashboard Catalog "Catalog":
    tile "Published products": count Product where published = true
    tile "Out of stock":       count Product where in_stock = 0
    tile "By category":        count Product group by category
