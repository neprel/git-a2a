project('consumer-app', 'cpp')

acme = subproject('acme-lib-utils')
executable('consumer-app', 'main.cpp', dependencies: acme.get_variable('acme_lib_utils_dep'))
