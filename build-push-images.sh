aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin 150105760238.dkr.ecr.us-east-1.amazonaws.com

# Cart
docker build -t cart ./src/cart
docker tag cart:latest 150105760238.dkr.ecr.us-east-1.amazonaws.com/cart:latest
docker push 150105760238.dkr.ecr.us-east-1.amazonaws.com/cart:latest

# Catalog
docker build -t catalog ./src/catalog
docker tag catalog:latest 150105760238.dkr.ecr.us-east-1.amazonaws.com/catalog:latest
docker push 150105760238.dkr.ecr.us-east-1.amazonaws.com/catalog:latest

# Checkout
docker build -t checkout ./src/checkout
docker tag checkout:latest 150105760238.dkr.ecr.us-east-1.amazonaws.com/checkout:latest
docker push 150105760238.dkr.ecr.us-east-1.amazonaws.com/checkout:latest

# Orders
docker build -t orders ./src/orders
docker tag orders:latest 150105760238.dkr.ecr.us-east-1.amazonaws.com/orders:latest
docker push 150105760238.dkr.ecr.us-east-1.amazonaws.com/orders:latest

# Recommendation
docker build -t recommendations ./src/recommendations
docker tag recommendations:latest 150105760238.dkr.ecr.us-east-1.amazonaws.com/recommendations:latest
docker push 150105760238.dkr.ecr.us-east-1.amazonaws.com/recommendations:latest

# UI
docker build -t ui ./src/ui
docker tag ui:latest 150105760238.dkr.ecr.us-east-1.amazonaws.com/ui:latest
docker push 150105760238.dkr.ecr.us-east-1.amazonaws.com/ui:latest
