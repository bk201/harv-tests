*** Settings ***
Library                SSHLibrary
Suite Setup            Open Connection And Log In
Suite Teardown         Close All Connections
Variables              /srv/www/htdocs/harvester/config-create.yaml
Variables              expected_values.yaml

*** Variables ***
${HOST}                10.10.0.11
${USERNAME}            rancher
${PASSWORD}            ***



*** Test Cases ***
Check partitions
    ${output}=         Execute Command    lsblk -n -l -J -o NAME,LABEL,MOUNTPOINT ${install}[device]
    Should Be Equal    ${output}          ${expected}[partitions]
Check hostname
    ${output}=         Execute Command    hostname
    Should Be Equal    ${output}          ${os}[hostname]
Check rancherd token
    ${output}=         Execute Command    yq e .token /etc/rancher/rancherd/config.yaml   sudo=True
    Should Be Equal    ${output}          ${token}
Check RKE2 token
    ${output}=         Execute Command    yq e .token /etc/rancher/rke2/config.yaml.d/40-rancherd.yaml    sudo=True
    Should Be Equal    ${output}          ${token}

*** Keywords ***
Open Connection And Log In
    Open Connection     ${HOST}
    Login               ${USERNAME}        ${PASSWORD}
