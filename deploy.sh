docker build ./ -t registry.digitalocean.com/prod2/tickets-frontend:respond; docker push registry.digitalocean.com/prod2/tickets-frontend:respond
helm upgrade -i respond -n respond ./charts/respond/  -f ../helm-values/respond.yaml  --debug
