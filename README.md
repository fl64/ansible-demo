# Cloud init ansible demo for Deckhouse Virtualization VM

```bash
# Create VM
kubectl create -f ./vm

# Connect to VM
# user / pass = ansible / ansible
d8 v console -n ansible ansible-demo

# Check created via ansible file
cat /tmp/ansible_test_file.txt

# Remove VM
kubectl delete -f ./vm
```

Output example:

```txt
[   67.344144] cloud-init[1042]: PLAY [Test Ansible Playbook] ***************************************************
[   67.347155] cloud-init[1042]: TASK [Gathering Facts] *********************************************************
[   67.350132] cloud-init[1042]: ok: [localhost]
[   67.350510] cloud-init[1042]: TASK [Update apt cache] ********************************************************
[   67.353202] cloud-init[1042]: changed: [localhost]
[   67.353985] cloud-init[1042]: TASK [Update yum cache] ********************************************************
[   67.356071] cloud-init[1042]: skipping: [localhost]
[   67.358012] cloud-init[1042]: TASK [Update apk cache (Alpine)] ***********************************************
[   67.358814] cloud-init[1042]: skipping: [localhost]
[   67.359229] cloud-init[1042]: TASK [Create test file] ********************************************************
[   67.361016] cloud-init[1042]: changed: [localhost]
[   67.363009] cloud-init[1042]: TASK [Write test content to file] **********************************************
[   67.363783] cloud-init[1042]: changed: [localhost]
[   67.364193] cloud-init[1042]: PLAY RECAP *********************************************************************
[   67.366009] cloud-init[1042]: localhost                  : ok=4    changed=3    unreachable=0    failed=0    skipped=2    rescued=0    ignored=0
[   67.368117] cloud-init[1042]: Starting Ansible Pull at 2025-11-20 06:29:48
[   67.368692] cloud-init[1042]: /usr/bin/ansible-pull --url=https://github.com/fl64/ansible-demo.git test-playbook.yml
```
